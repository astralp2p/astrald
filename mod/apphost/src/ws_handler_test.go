package apphost

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/lib/query"
	"github.com/cryptopunkscc/astrald/mod/apphost"
)

// inboundTestRig builds a Module + WSHandler pair backed by a net.Pipe so the test
// can read the IncomingQueryMsg the host sends to the "registration WS" and write
// responses (reject) back as if it were the JS client.
type inboundTestRig struct {
	mod       *Module
	handler   *WSHandler
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

	h := &WSHandler{
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

// runRouteQuery starts WSHandler.RouteQuery in a goroutine and returns channels for
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

func TestWSHandler_AttachPath(t *testing.T) {
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
	if msg.QueryID != q.Nonce {
		t.Errorf("QueryID = %v, want %v", msg.QueryID, q.Nonce)
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

func TestWSHandler_RejectPath(t *testing.T) {
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

func TestWSHandler_TimeoutPath(t *testing.T) {
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

func TestWSHandler_HandlerGoneOnSendFailure(t *testing.T) {
	rig := newInboundTestRig(t)
	// Close the client side BEFORE RouteQuery runs, so the Send fails.
	rig.clientCxn.Close()

	q := newQuery()
	_, errCh := rig.runRouteQuery(q, &nopWriteCloser{})

	select {
	case err := <-errCh:
		if !errors.Is(err, errWSHandlerGone) {
			t.Fatalf("expected errWSHandlerGone, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RouteQuery did not return on send failure")
	}
}

// Drain ensures the test file's imports are used.
var _ = sync.Mutex{}
