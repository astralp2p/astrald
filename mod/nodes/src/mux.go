package nodes

import (
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/api/nodes"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astral-go/lib/query"
	"github.com/cryptopunkscc/astrald/mod/nodes/frames"
)

// Mux multiplexes many query sessions over one link channel, dispatching inbound frames and tracking per-session state.
type Mux struct {
	mod *Module
	ch  *channel.Channel

	linkID         astral.Nonce
	currentLink    func() *Link
	onBytes        func(int)
	onPong         func(astral.Nonce) (time.Duration, error)
	localIdentity  *astral.Identity
	remoteIdentity *astral.Identity

	sessions sig.Map[astral.Nonce, *session]

	router     astral.Router
	routerSet  chan struct{}
	routerOnce atomic.Bool
}

func newMux(
	mod *Module,
	ch *channel.Channel,
	localIdentity *astral.Identity,
	remoteIdentity *astral.Identity,
	onBytes func(int),
	onPong func(astral.Nonce) (time.Duration, error),
) *Mux {
	m := &Mux{
		mod:            mod,
		ch:             ch,
		onBytes:        onBytes,
		onPong:         onPong,
		localIdentity:  localIdentity,
		remoteIdentity: remoteIdentity,
		routerSet:      make(chan struct{}),
	}
	return m
}

func (m *Mux) LocalIdentity() *astral.Identity {
	return m.localIdentity
}

func (m *Mux) RemoteIdentity() *astral.Identity {
	return m.remoteIdentity
}

func (m *Mux) SendMigrateFrame(nonce astral.Nonce) error {
	return m.ch.Send(&frames.Migrate{Nonce: nonce})
}

// SetRouter installs the router once; subsequent calls are ignored. Unblocks waitRouter.
func (m *Mux) SetRouter(r astral.Router) {
	if !m.routerOnce.CompareAndSwap(false, true) {
		return
	}
	m.router = r
	close(m.routerSet)
}

// RouteQuery opens a session and sends a query (or relay query) frame, then blocks for the peer's routing result or ctx cancellation; on accept it pumps the session into w.
func (m *Mux) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (_ io.WriteCloser, err error) {
	sourceID := m.RemoteIdentity()
	if q.Caller.IsEqual(ctx.Identity()) {
		sourceID = nil
	}

	conn, ok := m.createSession(q.Nonce, q.Target, sourceID, q.QueryString, true, 0)
	if !ok {
		return query.RouteNotFound()
	}

	queryFrame := frames.Query{
		Nonce:  q.Nonce,
		Query:  q.QueryString,
		Buffer: uint32(defaultBufferSize),
	}

	var frame frames.Frame = &queryFrame
	if !q.Caller.IsEqual(ctx.Identity()) {
		frame = &frames.RelayQuery{
			CallerID: q.Caller,
			TargetID: q.Target,
			Query:    queryFrame,
		}
	}

	if err := m.ch.Send(frame); err != nil {
		conn.Close()
		return nil, err
	}

	select {
	case errCode := <-conn.routingResult:
		if errCode != 0 {
			return query.RejectWithCode(errCode)
		}

		go func() {
			io.Copy(w, conn)
			w.Close()
		}()

		return conn, nil

	case <-ctx.Done():
		conn.Close()
		return query.RouteNotFound()
	}
}

// Handle dispatches one inbound frame; query frames are handled in their own goroutine, the rest inline on the read loop.
func (m *Mux) Handle(obj astral.Object) error {
	frame, ok := obj.(frames.Frame)
	if !ok {
		return fmt.Errorf("unknown object type: %v", obj.ObjectType())
	}
	switch f := frame.(type) {
	case *frames.Query:
		go m.handleQuery(f)
	case *frames.RelayQuery:
		go m.handleRelayQuery(f)
	case *frames.Response:
		m.handleResponse(f)
	case *frames.Ping:
		m.handlePing(f)
	case *frames.Data:
		m.handleData(f)
	case *frames.Reset:
		m.handleReset(f)
	case *frames.Read:
		m.handleRead(f)
	case *frames.Migrate:
		m.handleMigrate(f)
	default:
		return fmt.Errorf("unhandled frame type: %T", frame)
	}
	return nil
}

func (m *Mux) handleQuery(f *frames.Query) {
	m.handleInboundQuery(f.Nonce, m.RemoteIdentity(), m.LocalIdentity(), nil, f.Query, int(f.Buffer))
}

func (m *Mux) handleRelayQuery(relayQuery *frames.RelayQuery) error {
	if !relayQuery.CallerID.IsEqual(m.RemoteIdentity()) {
		ctx := astral.NewContext(nil).WithIdentity(m.mod.node.Identity())
		if !m.mod.Auth.Authorize(ctx, &nodes.RelayForAction{
			Action: auth.NewAction(m.RemoteIdentity()),
			ForID:  relayQuery.CallerID,
		}) {
			_ = m.ch.Send(&frames.Response{Nonce: relayQuery.Query.Nonce, ErrCode: frames.CodeRejected})
			return nil
		}
	}

	m.handleInboundQuery(
		relayQuery.Query.Nonce,
		relayQuery.CallerID,
		relayQuery.TargetID,
		m.RemoteIdentity(),
		relayQuery.Query.Query,
		int(relayQuery.Query.Buffer),
	)

	return nil
}

func (m *Mux) handleInboundQuery(linkNonce astral.Nonce, caller, target, relayID *astral.Identity, queryStr string, initBuffer int) {
	conn, ok := m.createSession(linkNonce, caller, relayID, queryStr, false, initBuffer)
	if !ok {
		return
	}

	ctx := astral.NewContext(nil).WithIdentity(m.mod.node.Identity())

	router, err := m.waitRouter(ctx)
	if err != nil {
		conn.Close()
		m.ch.Send(&frames.Response{Nonce: linkNonce, ErrCode: frames.CodeRejected})
		return
	}

	q := astral.Launch(&astral.Query{
		Nonce:       linkNonce,
		Caller:      caller,
		Target:      target,
		QueryString: queryStr,
	})

	q.Extra.Set("origin", astral.OriginNetwork)
	w, err := router.RouteQuery(ctx, q, conn)
	if err != nil {
		conn.Close()
		code := uint8(frames.CodeRejected)
		var reject *astral.ErrRejected
		if errors.As(err, &reject) {
			code = reject.Code
		}
		m.ch.Send(&frames.Response{Nonce: linkNonce, ErrCode: code})
		return
	}

	conn.setState(stateOpen)
	m.ch.Send(&frames.Response{Nonce: linkNonce, ErrCode: frames.CodeAccepted, Buffer: uint32(defaultBufferSize)})
	conn.Open()

	go func() {
		io.Copy(w, conn)
		w.Close()
	}()
}

func (m *Mux) handleResponse(f *frames.Response) {
	conn, ok := m.sessions.Get(f.Nonce)
	if !ok {
		return
	}

	if conn.SourceIdentity != nil && !conn.SourceIdentity.IsEqual(m.RemoteIdentity()) {
		return
	}

	if conn.SourceIdentity == nil && !conn.RemoteIdentity.IsEqual(m.RemoteIdentity()) {
		return
	}

	if f.ErrCode != 0 {
		if !conn.swapState(stateRouting, stateClosed) {
			return
		}
		conn.routingResult <- f.ErrCode
		return
	}

	if !conn.swapState(stateRouting, stateOpen) {
		return
	}

	if writer, ok := conn.writer.(*muxSessionWriter); ok {
		writer.Grow(int(f.Buffer))
	}
	conn.Open()

	conn.routingResult <- 0
}

func (m *Mux) handleData(f *frames.Data) {
	session, ok := m.sessions.Get(f.Nonce)
	if !ok {
		m.ch.Send(&frames.Reset{Nonce: f.Nonce})
		return
	}

	switch session.getState() {
	case stateOpen, stateMigrating:
	default:
		m.mod.log.Errorv(1, "received data frame from %v in state %v", m.RemoteIdentity(), session.getState())
		m.ch.Send(&frames.Reset{Nonce: f.Nonce})
		return
	}

	if m.onBytes != nil {
		m.onBytes(len(f.Payload))
	}

	reader, ok := session.reader.(*muxSessionReader)
	if !ok {
		m.mod.log.Errorv(1, "received data frame from %v on non-mux session", m.RemoteIdentity())
		return
	}

	if err := reader.Push(f.Payload); err != nil {
		m.mod.log.Errorv(1, "failed to push read frame: %v", err)
		session.Close()
		return
	}
}

func (m *Mux) handleRead(f *frames.Read) {
	session, ok := m.sessions.Get(f.Nonce)
	if !ok {
		m.ch.Send(&frames.Reset{Nonce: f.Nonce})
		return
	}

	writer, ok := session.writer.(*muxSessionWriter)
	if ok {
		writer.Grow(int(f.Len))
	}
}

func (m *Mux) handleReset(f *frames.Reset) {
	session, ok := m.sessions.Get(f.Nonce)
	if !ok {
		return
	}

	if w, ok := session.writer.(*muxSessionWriter); ok {
		w.PeerClose()
	}

	session.Close()
}

func (m *Mux) handlePing(f *frames.Ping) {
	if f.Pong {
		rtt, err := m.onPong(f.Nonce)
		if err != nil {
			m.mod.log.Errorv(1, "invalid pong nonce from %v", m.RemoteIdentity())
		} else if m.mod.config.LogPings {
			m.mod.log.Logv(1, "ping with %v: %v", m.RemoteIdentity(), rtt)
		}
		return
	}

	m.ch.Send(&frames.Ping{
		Nonce: f.Nonce,
		Pong:  true,
	})
}

func (m *Mux) handleMigrate(f *frames.Migrate) {
	sess, ok := m.sessions.Get(f.Nonce)
	if !ok {
		return
	}

	if sess.getState() != stateMigrating {
		return
	}

	reader, ok := sess.reader.(*muxSessionReader)
	if !ok {
		m.mod.log.Errorv(1, "received migrate frame on non-mux session")
		return
	}

	reader.Advance()
}

// waitRouter blocks until the mux has a router or ctx is cancelled.
func (m *Mux) waitRouter(ctx *astral.Context) (astral.Router, error) {
	select {
	case <-m.routerSet:
		return m.router, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
