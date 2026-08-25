package mcp

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	authapi "github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// fakeAuth answers Authorize from a field and nothing else.
//
// why the interface is embedded rather than implemented: mcp asks auth three
// questions and a test double for all ten would say the module depends on more
// than it does. A method this reaches without a stub panics, which is the report
// that the dependency grew.
type fakeAuth struct {
	authmod.Module
	allow bool
	asked []authapi.ActionObject
}

func (f *fakeAuth) Authorize(_ *astral.Context, action authapi.ActionObject) bool {
	f.asked = append(f.asked, action)
	return f.allow
}

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
	return testRouterModuleWithAuth(t, &fakeAuth{allow: true})
}

// testRouterModuleWithAuth builds the router over an authority that answers as
// the test says. Every existing case runs under one that allows, so what they
// cover is unchanged.
func testRouterModuleWithAuth(t *testing.T, auth *fakeAuth) *Module {
	t.Helper()
	return &Module{
		Deps: Deps{Auth: auth},
		ctx:  astral.NewContext(nil),
		config: Config{
			SessionTTL:         time.Second,
			PayloadReadWindow:  50 * time.Millisecond,
			PendingTTL:         500 * time.Millisecond,
			MaxPending:         2,
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
	_ = mod.agentIDs.Add(agentID.String())

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
	_ = mod.agentIDs.Add(agentID.String())

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
	_ = mod.agentIDs.Add(agentID.String())

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

// waitPending blocks until the agent has n queued queries.
func waitPending(t *testing.T, mod *Module, agentID *astral.Identity, n int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if mod.pendingCount(agentID) == n {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("pending count %v, want %v", mod.pendingCount(agentID), n)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestRouteQueryQueuesForLateListener(t *testing.T) {
	mod := testRouterModule(t)
	agentID := astral.GenerateIdentity()
	_ = mod.agentIDs.Add(agentID.String())

	// no listener parked — the query is accepted and queued
	w := &bufWriteCloser{}
	wc, err := mod.RouteQuery(mod.ctx, inFlight(agentID, "chat?topic=test"), w)
	if err != nil {
		t.Fatalf("route with no listener: %v", err)
	}
	wc.Write([]byte("hello"))

	waitPending(t, mod, agentID, 1)

	s, ok := mod.takePending(agentID)
	if !ok {
		t.Fatal("pending query not delivered")
	}
	if s.path != "chat" || string(s.payload) != "hello" {
		t.Fatalf("path %q payload %q", s.path, s.payload)
	}

	if _, err = s.send([]byte("answer")); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if w.String() != "answer" {
		t.Fatalf("caller read %q", w.String())
	}
	mod.closeSession(s.id)
}

func TestPendingExpires(t *testing.T) {
	mod := testRouterModule(t)
	agentID := astral.GenerateIdentity()
	_ = mod.agentIDs.Add(agentID.String())

	if _, err := mod.RouteQuery(mod.ctx, inFlight(agentID, "chat"), &bufWriteCloser{}); err != nil {
		t.Fatalf("route: %v", err)
	}
	waitPending(t, mod, agentID, 1)

	time.Sleep(700 * time.Millisecond)

	if _, ok := mod.takePending(agentID); ok {
		t.Fatal("expired query still queued")
	}
	if mod.sessions.Len() != 0 {
		t.Fatalf("%v sessions left after expiry", mod.sessions.Len())
	}
}

func TestPendingQueueFull(t *testing.T) {
	mod := testRouterModule(t)
	agentID := astral.GenerateIdentity()
	_ = mod.agentIDs.Add(agentID.String())

	for i := 0; i < mod.config.MaxPending; i++ {
		if _, err := mod.RouteQuery(mod.ctx, inFlight(agentID, "chat"), &bufWriteCloser{}); err != nil {
			t.Fatalf("route %v: %v", i, err)
		}
	}
	waitPending(t, mod, agentID, mod.config.MaxPending)

	_, err := mod.RouteQuery(mod.ctx, inFlight(agentID, "chat"), &bufWriteCloser{})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("route on full queue: got %v, want route not found", err)
	}

	mod.dropPending(agentID)
	if mod.pendingCount(agentID) != 0 {
		t.Fatal("queue not dropped")
	}
}

func TestRouteQueryNoQueueForUnknownTarget(t *testing.T) {
	mod := testRouterModule(t)

	_, err := mod.RouteQuery(mod.ctx, inFlight(astral.GenerateIdentity(), "chat"), &bufWriteCloser{})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("route: got %v, want route not found", err)
	}
}

func TestUnparkKeepsFreshListener(t *testing.T) {
	mod := testRouterModule(t)
	agentID := astral.GenerateIdentity()
	_ = mod.agentIDs.Add(agentID.String())

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

// An agent nobody opted in is unreachable, and unreachable the way an absent
// identity is: RouteNotFound is non-terminal, so the answer does not confirm

// Visibility alone routes: registration is what makes an agent queue, and the
// listener is what makes it answer, but neither is permission.
func TestRouteQueryAdmitsRegisteredAgent(t *testing.T) {
	mod := testRouterModule(t)
	agentID := astral.GenerateIdentity()
	_ = mod.agentIDs.Add(agentID.String())

	wc, err := mod.RouteQuery(mod.ctx, inFlight(agentID, "chat"), &bufWriteCloser{})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	wc.Close()
}
