package apps

import (
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	libapphost "github.com/cryptopunkscc/astrald/lib/apphost"
	"github.com/cryptopunkscc/astrald/lib/ipc"
	"github.com/cryptopunkscc/astrald/mod/apphost"
)

// Handler accepts inbound IPC queries from an apphost-registered endpoint.
// Close or context cancellation terminates all blocking calls.
type Handler struct {
	listener net.Listener
	ipcToken astral.Nonce
	doneCh   chan struct{}
	done     atomic.Bool
}

// NewHandler creates an IPC listener with a random auth token.
// Registration with the node is the caller's responsibility.
func NewHandler() (*Handler, error) {
	return NewHandlerAt(libapphost.DefaultRouter().Protocol(), astral.NewNonce())
}

// NewHandlerAt creates an IPC listener for the given protocol and auth token.
func NewHandlerAt(protocol string, token astral.Nonce) (*Handler, error) {
	l, err := ipc.ListenAny(protocol)
	if err != nil {
		return nil, err
	}
	return &Handler{
		listener: l,
		doneCh:   make(chan struct{}),
		ipcToken: token,
	}, nil
}

// ReadQuery waits for and returns the next pending query
func (h *Handler) ReadQuery() (*PendingQuery, error) {
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			h.Close()
			return nil, err
		}
		ch := channel.New(conn)

		obj, err := ch.Receive()
		if err != nil {
			ch.Close()
			continue
		}

		// check message type - must be HandleQueryMsg
		queryMsg, ok := obj.(*apphost.HandleQueryMsg)
		if !ok {
			ch.Send(&apphost.ErrorMsg{Code: apphost.ErrCodeProtocolError})
			ch.Close()
			continue
		}

		// check auth ipcToken
		if queryMsg.IPCToken != h.ipcToken {
			ch.Send(&apphost.ErrorMsg{Code: apphost.ErrCodeDenied})
			ch.Close()
			continue
		}

		// return the pending query
		return &PendingQuery{
			conn: conn,
			query: &astral.Query{
				Nonce:       queryMsg.ID,
				Caller:      queryMsg.Caller,
				Target:      queryMsg.Target,
				QueryString: string(queryMsg.Query),
			},
		}, nil
	}
}

type HandleFunc func(ctx *astral.Context, query *PendingQuery) error

// Serve calls the given HandleFunc for every query received
func (h *Handler) Serve(ctx *astral.Context, handle HandleFunc) error {
	stop := h.closeOnDone(ctx)
	defer stop()

	for {
		pending, err := h.ReadQuery()
		if err != nil {
			if ctx.Err() != nil && isClosedListenerErr(err) {
				return ctx.Err()
			}
			return err
		}

		err = handle(ctx, pending)
		if err != nil {
			return err
		}
	}
}

// Route routes every query to given astral.Router
func (h *Handler) Route(ctx *astral.Context, router astral.Router) error {
	var errRejected *astral.ErrRejected

	stop := h.closeOnDone(ctx)
	defer stop()

	for {
		// get the next pending query
		pending, err := h.ReadQuery()
		if err != nil {
			if ctx.Err() != nil && isClosedListenerErr(err) {
				return ctx.Err()
			}
			return err
		}

		// lock the writer so that we can send the query response before the target starts sending data
		lockedWriter := newLockableWriteCloser(pending.conn)
		lockedWriter.Lock()

		// route the query to the target
		w, err := router.RouteQuery(ctx, astral.Launch(pending.query), lockedWriter)
		switch {
		case err == nil:
			// accepted - send an Ack and release the writer to the query target
			conn := pending.Accept()
			lockedWriter.Unlock()

			// forward the traffic
			go func() {
				io.Copy(w, conn)
				w.Close()
			}()
		case errors.As(err, &errRejected):
			// rejected - forward the rejection code and release the writer
			pending.RejectWithCode(int(errRejected.Code))
			lockedWriter.Unlock()

		default:
			pending.Skip()
			lockedWriter.Unlock()
		}
	}
}

// Token returns the token expected by the Handler
func (h *Handler) Token() astral.Nonce {
	return h.ipcToken
}

func (h *Handler) Close() error {
	if h.done.CompareAndSwap(false, true) {
		close(h.doneCh)
	}
	return h.listener.Close()
}

func (h *Handler) closeOnDone(ctx *astral.Context) func() {
	done := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			_ = h.Close()
		case <-done:
		}
	}()

	return func() { close(done) }
}

// todo: weird error checking - look for better solution
func isClosedListenerErr(err error) bool {
	return errors.Is(err, net.ErrClosed) ||
		strings.Contains(err.Error(), "use of closed network connection") ||
		strings.Contains(err.Error(), "connection closed")
}

func (h *Handler) String() string {
	return h.Endpoint()
}

func (h *Handler) Endpoint() string {
	a := h.listener.Addr()
	return a.Network() + ":" + a.String()
}

// Done returns a channel that will be closed when the Handler is closed
func (h *Handler) Done() <-chan struct{} {
	return h.doneCh
}
