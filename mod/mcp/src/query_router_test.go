package mcp

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

type bufWriteCloser struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *bufWriteCloser) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *bufWriteCloser) Close() error { return nil }

func (b *bufWriteCloser) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func testRouterModule(t *testing.T) *Module {
	t.Helper()
	return &Module{
		ctx: astral.NewContext(nil),
		config: Config{
			SessionTTL:         time.Second,
			PayloadReadWindow:  50 * time.Millisecond,
			ListenGrace:        200 * time.Millisecond,
			MaxPayloadBytes:    64 << 10,
			MaxResponseBytes:   64 << 10,
			MaxResponseObjects: 64,
		},
	}
}

func inFlight(target *astral.Identity, qs string) *astral.InFlightQuery {
	return astral.Launch(astral.NewQuery(astral.GenerateIdentity(), target, qs))
}

func TestRouteQueryNoListener(t *testing.T) {
	mod := testRouterModule(t)

	_, err := mod.RouteQuery(mod.ctx, inFlight(astral.GenerateIdentity(), "chat"), &bufWriteCloser{})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("route: got %v, want route not found", err)
	}
}

func TestRouteQueryDeliversSession(t *testing.T) {
	mod := testRouterModule(t)
	agentID := astral.GenerateIdentity()

	ch, err := mod.parkListener(agentID)
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	defer mod.unparkListener(agentID, ch)

	w := &bufWriteCloser{}
	wc, err := mod.RouteQuery(mod.ctx, inFlight(agentID, "chat?topic=test"), w)
	if err != nil {
		t.Fatalf("route: %v", err)
	}

	if _, err = wc.Write([]byte("hello")); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var s *session
	select {
	case s = <-ch:
	case <-time.After(time.Second):
		t.Fatal("no session delivered")
	}

	if s.path != "chat" || s.params["topic"] != "test" {
		t.Fatalf("parsed path %q params %v", s.path, s.params)
	}
	if string(s.payload) != "hello" {
		t.Fatalf("payload %q", s.payload)
	}

	if _, err = s.send([]byte("world")); err != nil {
		t.Fatalf("send response: %v", err)
	}
	if w.String() != "world" {
		t.Fatalf("caller read %q", w.String())
	}

	mod.closeSession(s.id)
}

func TestParkListenerTwice(t *testing.T) {
	mod := testRouterModule(t)
	agentID := astral.GenerateIdentity()

	ch, err := mod.parkListener(agentID)
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	defer mod.unparkListener(agentID, ch)

	if _, err = mod.parkListener(agentID); !errors.Is(err, errAlreadyListening) {
		t.Fatalf("second park: got %v, want errAlreadyListening", err)
	}
}

func TestUnparkClosesLateSession(t *testing.T) {
	mod := testRouterModule(t)
	agentID := astral.GenerateIdentity()

	ch, err := mod.parkListener(agentID)
	if err != nil {
		t.Fatalf("park: %v", err)
	}

	_, err = mod.RouteQuery(mod.ctx, inFlight(agentID, "chat"), &bufWriteCloser{})
	if err != nil {
		t.Fatalf("route: %v", err)
	}

	// wait for the payload window so the session lands in ch
	time.Sleep(150 * time.Millisecond)

	mod.unparkListener(agentID, ch)

	if mod.sessions.Len() != 0 {
		t.Fatalf("%v sessions still tracked after unpark", mod.sessions.Len())
	}
}

func TestRouteQueryGraceDeliversAfterPark(t *testing.T) {
	mod := testRouterModule(t)
	agentID := astral.GenerateIdentity()
	_ = mod.agentIDs.Add(agentID.String())

	done := make(chan error, 1)
	go func() {
		_, err := mod.RouteQuery(mod.ctx, inFlight(agentID, "chat"), &bufWriteCloser{})
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	ch, err := mod.parkListener(agentID)
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	defer mod.unparkListener(agentID, ch)

	if err = <-done; err != nil {
		t.Fatalf("route during grace: %v", err)
	}

	select {
	case s := <-ch:
		mod.closeSession(s.id)
	case <-time.After(time.Second):
		t.Fatal("no session delivered to the late listener")
	}
}

func TestRouteQueryGraceExpires(t *testing.T) {
	mod := testRouterModule(t)
	agentID := astral.GenerateIdentity()
	_ = mod.agentIDs.Add(agentID.String())

	start := time.Now()
	_, err := mod.RouteQuery(mod.ctx, inFlight(agentID, "chat"), &bufWriteCloser{})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("route: got %v, want route not found", err)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("returned before the grace window expired")
	}
}

func TestRouteQueryNoGraceForUnknownTarget(t *testing.T) {
	mod := testRouterModule(t)

	start := time.Now()
	_, err := mod.RouteQuery(mod.ctx, inFlight(astral.GenerateIdentity(), "chat"), &bufWriteCloser{})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("route: got %v, want route not found", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("waited the grace window for an unregistered target")
	}
}

func TestUnparkKeepsFreshListener(t *testing.T) {
	mod := testRouterModule(t)
	agentID := astral.GenerateIdentity()

	ch1, _ := mod.parkListener(agentID)

	// a query claims ch1, then a fresh listen parks ch2
	if _, err := mod.RouteQuery(mod.ctx, inFlight(agentID, "chat"), &bufWriteCloser{}); err != nil {
		t.Fatalf("route: %v", err)
	}
	if _, err := mod.parkListener(agentID); err != nil {
		t.Fatalf("second park: %v", err)
	}

	mod.unparkListener(agentID, ch1)

	if _, ok := mod.popListener(agentID); !ok {
		t.Fatal("fresh listener was removed by stale unpark")
	}
}
