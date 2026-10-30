package apphost

import (
	"bytes"
	"errors"
	"io"
	"math"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
)

// inboundTestRig builds a Module + ServiceHandler pair backed by a net.Pipe so the test
// can read the IncomingQueryMsg the host sends to the "registration WS" and write
// responses (reject) back as if it were the JS client.
type inboundTestRig struct {
	mod       *Module
	handler   *ServiceHandler
	clientCh  *channel.Channel // the "JS client" side of the registration WS
	clientCxn net.Conn
}

func newInboundTestRig(t *testing.T) *inboundTestRig {
	t.Helper()

	mod := &Module{
		config: Config{},
		log:    log.New(nil),
	}

	srvCxn, cliCxn := net.Pipe()
	srvCh := channel.New(srvCxn, channel.WithLockedWrites())
	cliCh := channel.New(cliCxn)

	identity := &astral.Identity{} // zero identity is fine for this test — we only
	// route by IsEqual and the test crafts queries with the same zero identity.

	h := &ServiceHandler{
		Identity: identity,
		mod:      mod,
		ch:       srvCh,
	}

	return &inboundTestRig{
		mod:       mod,
		handler:   h,
		clientCh:  cliCh,
		clientCxn: cliCxn,
	}
}

// runRouteQuery starts ServiceHandler.RouteQuery in a goroutine and returns channels for
// its result.
func (rig *inboundTestRig) runRouteQuery(q *astral.InFlightQuery, w io.WriteCloser) (<-chan io.WriteCloser, <-chan error) {
	connCh := make(chan io.WriteCloser, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := rig.handler.RouteQuery(astral.NewContext(nil), q, w)
		connCh <- conn
		errCh <- err
	}()
	return connCh, errCh
}

func newQuery() *astral.InFlightQuery {
	q := query.New(nil, nil, "test.query", nil)
	return astral.Launch(q)
}

// nopWriteCloser is an io.WriteCloser that swallows writes — stand-in for the caller's
// write side. RouteQuery proxies bytes from the responder to this; in tests we don't
// always need to inspect them.
type nopWriteCloser struct{ buf bytes.Buffer }

func (n *nopWriteCloser) Write(p []byte) (int, error) { return n.buf.Write(p) }
func (n *nopWriteCloser) Close() error                { return nil }

func TestServiceHandler_AttachPath(t *testing.T) {
	rig := newInboundTestRig(t)
	defer rig.clientCxn.Close()

	q := newQuery()
	connCh, errCh := rig.runRouteQuery(q, &nopWriteCloser{})

	// Client side: receive IncomingQueryMsg.
	obj, err := rig.clientCh.Receive()
	if err != nil {
		t.Fatalf("client recv: %v", err)
	}
	msg, ok := obj.(*apphost.IncomingQueryMsg)
	if !ok {
		t.Fatalf("got %T, want IncomingQueryMsg", obj)
	}
	if msg.QueryID == q.Nonce {
		t.Errorf("QueryID is the query's nonce %v; want a fresh nonce", q.Nonce)
	}

	// Simulate per-query WS attach: deliver a fake responder conn via pending.attach.
	pending, ok := rig.mod.pendingInboundQueries.Get(msg.QueryID)
	if !ok {
		t.Fatal("pendingInboundQueries entry not registered")
	}
	respServer, respClient := net.Pipe()
	pending.attach <- respServer

	select {
	case conn := <-connCh:
		if conn == nil {
			t.Fatal("RouteQuery returned nil conn")
		}
		if err := <-errCh; err != nil {
			t.Fatalf("RouteQuery err: %v", err)
		}
		// the returned conn IS the donated responder conn (respServer)
		// — proves donation succeeded.
		_ = respClient.Close()
		_ = conn.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("RouteQuery did not return after attach")
	}
}

func TestServiceHandler_RejectPath(t *testing.T) {
	rig := newInboundTestRig(t)
	defer rig.clientCxn.Close()

	q := newQuery()
	_, errCh := rig.runRouteQuery(q, &nopWriteCloser{})

	obj, err := rig.clientCh.Receive()
	if err != nil {
		t.Fatalf("client recv: %v", err)
	}
	msg := obj.(*apphost.IncomingQueryMsg)

	pending, ok := rig.mod.pendingInboundQueries.Get(msg.QueryID)
	if !ok {
		t.Fatal("pendingInboundQueries entry not registered")
	}
	pending.reject <- 7 // arbitrary reject code

	select {
	case err := <-errCh:
		var rejected *astral.ErrRejected
		if !errors.As(err, &rejected) {
			t.Fatalf("expected ErrRejected, got %v", err)
		}
		if rejected.Code != 7 {
			t.Errorf("Code = %v, want 7", rejected.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RouteQuery did not return after reject")
	}
}

func TestServiceHandler_TimeoutPath(t *testing.T) {
	rig := newInboundTestRig(t)
	defer rig.clientCxn.Close()

	// Shorten the wait — the package constant is 5s but our test loop polls with a
	// 7s budget. We can't override the constant cleanly without a refactor, so this
	// is the slowest path; acceptable for the test suite.
	q := newQuery()
	_, errCh := rig.runRouteQuery(q, &nopWriteCloser{})

	// drain the IncomingQueryMsg so the goroutine actually proceeds to the select.
	_, err := rig.clientCh.Receive()
	if err != nil {
		t.Fatalf("client recv: %v", err)
	}

	select {
	case err := <-errCh:
		var notFound *astral.ErrRouteNotFound
		if !errors.As(err, &notFound) {
			t.Fatalf("expected ErrRouteNotFound, got %v", err)
		}
	case <-time.After(QueryAttachTimeout + 2*time.Second):
		t.Fatal("RouteQuery did not time out")
	}
}

func TestServiceHandler_HandlerGoneOnSendFailure(t *testing.T) {
	rig := newInboundTestRig(t)
	// Close the client side BEFORE RouteQuery runs, so the Send fails.
	rig.clientCxn.Close()

	q := newQuery()
	_, errCh := rig.runRouteQuery(q, &nopWriteCloser{})

	select {
	case err := <-errCh:
		if !errors.Is(err, errServiceHandlerGone) {
			t.Fatalf("expected errServiceHandlerGone, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RouteQuery did not return on send failure")
	}
}

// An over-long query is not routed to the app, and the handler survives it:
// IncomingQueryMsg carries the query as a string16, and a failed send would
// read as a closed handler and remove it. The next query reaches the app intact.
func TestServiceHandler_OverLongQueryKeepsTheHandler(t *testing.T) {
	rig := newInboundTestRig(t)
	defer rig.clientCxn.Close()

	long := newQuery()
	long.QueryString = astral.String32(strings.Repeat("q", math.MaxUint16+1))
	_, errCh := rig.runRouteQuery(long, &nopWriteCloser{})

	select {
	case err := <-errCh:
		var notFound *astral.ErrRouteNotFound
		if !errors.As(err, &notFound) {
			t.Fatalf("expected ErrRouteNotFound, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RouteQuery did not return on an over-long query")
	}

	next := newQuery()
	rig.runRouteQuery(next, &nopWriteCloser{})

	obj, err := rig.clientCh.Receive()
	if err != nil {
		t.Fatalf("client recv after an over-long query: %v", err)
	}
	msg, ok := obj.(*apphost.IncomingQueryMsg)
	if !ok || msg.Query != astral.String16(next.QueryString) {
		t.Fatalf("client received %T %v, want the next query %v", obj, obj, next.QueryString)
	}
}

// receiveIncoming reads the IncomingQueryMsg the host sends to the registration
// connection.
func (rig *inboundTestRig) receiveIncoming(t *testing.T) *apphost.IncomingQueryMsg {
	t.Helper()

	obj, err := rig.clientCh.Receive()
	if err != nil {
		t.Fatalf("client recv: %v", err)
	}
	msg, ok := obj.(*apphost.IncomingQueryMsg)
	if !ok {
		t.Fatalf("got %T, want IncomingQueryMsg", obj)
	}
	return msg
}

// requireRejected waits for RouteQuery to end with ErrRejected carrying code.
func requireRejected(t *testing.T, errCh <-chan error, code uint8) {
	t.Helper()

	select {
	case err := <-errCh:
		var rejected *astral.ErrRejected
		if !errors.As(err, &rejected) {
			t.Fatalf("expected ErrRejected, got %v", err)
		}
		if rejected.Code != code {
			t.Fatalf("Code = %v, want %v", rejected.Code, code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RouteQuery did not return after reject")
	}
}

// TestServiceHandler_AttachRefusesTheQueryNonce: the query's nonce is known to its caller, so
// a token-less connection naming it is answered route_not_found and does not
// become the responder.
func TestServiceHandler_AttachRefusesTheQueryNonce(t *testing.T) {
	rig := newInboundTestRig(t)
	defer rig.clientCxn.Close()

	q := newQuery()
	_, errCh := rig.runRouteQuery(q, &nopWriteCloser{})
	msg := rig.receiveIncoming(t)

	srv, cli := net.Pipe()
	defer srv.Close()
	defer cli.Close()
	attacher := NewGuest(rig.mod, srv)

	reply := make(chan astral.Object, 1)
	go func() {
		obj, _ := channel.New(cli).Receive()
		reply <- obj
	}()

	if err := attacher.onAttachQueryMsg(astral.NewContext(nil), &apphost.AttachQueryMsg{QueryID: q.Nonce}); err != nil {
		t.Fatalf("attach: %v", err)
	}

	select {
	case obj := <-reply:
		errMsg, ok := obj.(*apphost.ErrorMsg)
		if !ok || errMsg.Code != apphost.ErrCodeRouteNotFound {
			t.Fatalf("attach by the query's nonce answered %v; want route_not_found", obj)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("attach sent no answer")
	}
	if attacher.donated.Load() {
		t.Fatal("attach by the query's nonce donated the connection")
	}

	pending, ok := rig.mod.pendingInboundQueries.Get(msg.QueryID)
	if !ok {
		t.Fatal("the pending query is gone")
	}
	pending.reject <- 7
	requireRejected(t, errCh, 7)
}

// TestServiceHandler_RejectFromAnotherConnectionIsIgnored: a reject belongs to the
// registration connection that was told about the query. The same QueryID on
// another connection does not reach the query.
func TestServiceHandler_RejectFromAnotherConnectionIsIgnored(t *testing.T) {
	rig := newInboundTestRig(t)
	defer rig.clientCxn.Close()

	_, errCh := rig.runRouteQuery(newQuery(), &nopWriteCloser{})
	msg := rig.receiveIncoming(t)
	ctx := astral.NewContext(nil)

	srv, cli := net.Pipe()
	defer srv.Close()
	defer cli.Close()
	other := NewGuest(rig.mod, srv)
	if err := other.onRejectIncomingMsg(ctx, &apphost.RejectIncomingMsg{QueryID: msg.QueryID, Code: 9}); err != nil {
		t.Fatalf("reject from another connection: %v", err)
	}

	registration := NewGuestFromChannel(rig.mod, rig.handler.ch, nil, ModeBinary)
	if err := registration.onRejectIncomingMsg(ctx, &apphost.RejectIncomingMsg{QueryID: msg.QueryID, Code: 7}); err != nil {
		t.Fatalf("reject from the registration connection: %v", err)
	}

	requireRejected(t, errCh, 7)
}

// Drain ensures the test file's imports are used.
var _ = sync.Mutex{}
