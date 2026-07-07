package mobile

import (
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/query"
	"github.com/cryptopunkscc/astrald/core"
)

// acceptTimeout limits how long a QueryHandler may take to resolve an
// inbound query before it's auto-rejected (mirrors the ops framework).
const acceptTimeout = 5 * time.Second

// QueryHandler serves astral queries on behalf of the local node. The
// platform wrapper registers handlers via Node.AddQueryHandler to expose
// services (e.g. the player protocol) at the node's own identity, next to
// the daemon's native ops.
//
// HandleQuery is called on a dedicated goroutine for every matching query
// and must resolve it by calling Accept or Reject within 5 seconds (the
// query is auto-rejected otherwise). After Accept, the returned connection
// may be served from any thread for as long as needed.
type QueryHandler interface {
	HandleQuery(q *InboundQuery)
}

// AddQueryHandler registers a handler for queries directed at the local
// node, matched by op name (the part of the query before '?'). A name
// ending in '.' registers a prefix: AddQueryHandler("player.", h) routes
// every player.* query to h, letting the wrapper implement a whole
// protocol with a single registration. An exact-name registration wins
// over a prefix one; among prefixes, the longest match wins.
//
// Effective immediately, also while the node is running; a later
// registration for the same name replaces the earlier one. Daemon modules
// take routing precedence for the op names they define.
func (n *Node) AddQueryHandler(name string, h QueryHandler) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.queryHandlers == nil {
		n.queryHandlers = make(map[string]QueryHandler)
	}
	n.queryHandlers[name] = h
}

// RemoveQueryHandler unregisters the handler for the given op name or
// prefix.
func (n *Node) RemoveQueryHandler(name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.queryHandlers, name)
}

func (n *Node) lookupQueryHandler(name string) QueryHandler {
	n.mu.Lock()
	defer n.mu.Unlock()

	if h, found := n.queryHandlers[name]; found {
		return h
	}

	// longest registered prefix (a name ending in '.') that matches
	var best string
	for prefix := range n.queryHandlers {
		if !strings.HasSuffix(prefix, ".") {
			continue
		}
		if strings.HasPrefix(name, prefix) && len(prefix) > len(best) {
			best = prefix
		}
	}
	if best != "" {
		return n.queryHandlers[best]
	}
	return nil
}

// InboundQuery is a query directed at the local node, offered to a
// registered QueryHandler. Exactly one of Accept or Reject must be called.
type InboundQuery struct {
	queryString string
	caller      string
	origin      string
	originWeb   string

	remoteWriter io.WriteCloser
	resolved     atomic.Bool
	response     chan inboundResponse
}

type inboundResponse struct {
	w   io.WriteCloser
	err error
}

func newInboundQuery(q *astral.InFlightQuery, w io.WriteCloser) *InboundQuery {
	iq := &InboundQuery{
		queryString:  q.QueryString,
		remoteWriter: w,
		response:     make(chan inboundResponse, 1),
	}
	if q.Caller != nil {
		iq.caller = q.Caller.String()
	}
	if o, found := q.Extra.Get("origin"); found {
		iq.origin, _ = o.(string)
	}
	if o, found := q.Extra.Get("origin-web"); found {
		iq.originWeb, _ = o.(string)
	}
	return iq
}

// Query returns the full query string, including parameters
// (e.g. "player.play?index=2").
func (iq *InboundQuery) Query() string { return iq.queryString }

// Caller returns the identity of the caller as a hex string.
func (iq *InboundQuery) Caller() string { return iq.caller }

// Origin reports where the query came from: "" for local callers
// (apphost guests), "network" for remote nodes.
func (iq *InboundQuery) Origin() string { return iq.origin }

// OriginWeb returns the browser Origin header for queries arriving over
// the apphost WebSocket endpoint, or "" for non-browser callers.
func (iq *InboundQuery) OriginWeb() string { return iq.originWeb }

// Accept accepts the query and returns its bidirectional connection.
// Returns nil if the query was already resolved (e.g. timed out).
func (iq *InboundQuery) Accept() *QueryConn {
	if !iq.resolved.CompareAndSwap(false, true) {
		return nil
	}
	pr, pw := io.Pipe()
	iq.response <- inboundResponse{w: pw}
	return &QueryConn{r: pr, w: iq.remoteWriter}
}

// Reject rejects the query with the default reject code. Safe to call
// after the query is resolved (no-op).
func (iq *InboundQuery) Reject() {
	if !iq.resolved.CompareAndSwap(false, true) {
		return
	}
	iq.response <- inboundResponse{err: &astral.ErrRejected{Code: astral.DefaultRejectCode}}
}

// QueryConn is the bidirectional byte stream of an accepted query. The
// handler writes its response objects and reads the caller's data in
// whatever encoding the protocol prescribes. Not safe for concurrent use
// of the same direction from multiple threads.
type QueryConn struct {
	r *io.PipeReader
	w io.WriteCloser
}

// Read fills buf with up to len(buf) bytes from the caller and returns
// the number of bytes read. A return of 0 means the caller closed the
// stream (gomobile maps Go errors to exceptions, so EOF is in-band).
func (c *QueryConn) Read(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	for {
		n, err := c.r.Read(buf)
		if n > 0 {
			return n, nil
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
				return 0, nil
			}
			return 0, err
		}
	}
}

// Write sends len(buf) bytes to the caller.
func (c *QueryConn) Write(buf []byte) (int, error) {
	return c.w.Write(buf)
}

// Close ends the query from the handler's side.
func (c *QueryConn) Close() error {
	c.r.Close()
	return c.w.Close()
}

// handlerRouter routes queries targeting the node identity to registered
// QueryHandlers. Installed into the node's priority router by Start.
type handlerRouter struct {
	node  *Node
	cnode *core.Node
}

var _ astral.Router = &handlerRouter{}

func (r *handlerRouter) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	if !q.Target.IsEqual(r.cnode.Identity()) {
		return query.RouteNotFound()
	}

	name, _ := query.Parse(q.QueryString)

	h := r.node.lookupQueryHandler(name)
	if h == nil {
		return query.RouteNotFound()
	}

	iq := newInboundQuery(q, w)

	go func() {
		// auto-reject in case the handler didn't resolve the query;
		// no-op if it did
		defer iq.Reject()
		h.HandleQuery(iq)
	}()

	select {
	case res := <-iq.response:
		return res.w, res.err

	case <-ctx.Done():
		iq.Reject()
		return nil, astral.NewErrRejected(astral.CodeCanceled)

	case <-time.After(acceptTimeout):
		iq.Reject()
		return nil, astral.NewErrRejected(astral.DefaultRejectCode)
	}
}

func (r *handlerRouter) String() string {
	return "mobile.wrapper"
}
