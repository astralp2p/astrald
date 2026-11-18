package apphost

import (
	"io"
	"net"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// Fixtures for the bind-session end on a failed dial: a handler whose endpoint
// refuses the dial is removed, and the session that bound its token ends, so
// the app reconnects and registers the handler again.

// deadEndpoint returns a tcp endpoint that refuses every dial.
func deadEndpoint(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close()

	return "tcp:" + addr
}

// liveBind is an apphost.bind session held open by a test.
type liveBind struct {
	conn io.WriteCloser
	w    *recordingWriter
}

// ended reports whether the op closed the session within wait.
func (b *liveBind) ended(wait time.Duration) bool {
	select {
	case <-b.w.closed:
		return true
	case <-time.After(wait):
		return false
	}
}

// openBind runs an apphost.bind session as principal - nil for a token-less
// session - binds token in it, and returns once the session recorded the token.
func openBind(t *testing.T, mod *Module, principal *astral.Identity, token astral.Nonce) *liveBind {
	t.Helper()

	op, err := routing.NewOp(mod.OpBind)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	w := newRecordingWriter()
	conn, err := op.RouteQuery(ctx, sessionQuery(mod, principal, principal, "apphost.bind"), w)
	if err != nil {
		t.Fatalf("apphost.bind refused %v: %v", principal, err)
	}
	t.Cleanup(func() { conn.Close() })

	if err = channel.NewSender(conn).Send(&apphost.BindMsg{Token: token}); err != nil {
		t.Fatalf("send bind message: %v", err)
	}

	b := &liveBind{conn: conn, w: w}
	deadline := time.Now().Add(5 * time.Second)
	for !b.bound(mod, principal, token) {
		if time.Now().After(deadline) {
			t.Fatal("the session never recorded its token")
		}
		time.Sleep(time.Millisecond)
	}

	return b
}

// bound reports whether a session of principal recorded token. A token-less
// session leaves no registry entry, so its token is taken as recorded once
// the op has read it.
func (b *liveBind) bound(mod *Module, principal *astral.Identity, token astral.Nonce) bool {
	if principal != nil {
		return mod.bindSessions.find(principal, token) != nil
	}
	time.Sleep(50 * time.Millisecond)
	return true
}

// routeTo routes one query to target through the module's router.
func routeTo(mod *Module, target *astral.Identity) error {
	ctx, cancel := astral.NewContext(nil).WithTimeout(5 * time.Second)
	defer cancel()

	q := astral.Launch(query.New(astral.GenerateIdentity(), target, "test.op", nil))
	_, err := mod.RouteQuery(ctx, q, &nopWriteCloser{})
	return err
}

// TestFailedDialEndsOnlyTheOwnersSession: two apps bind the same token value;
// a failed dial of P's handler ends P's session, and Q's session and handler
// stay.
func TestFailedDialEndsOnlyTheOwnersSession(t *testing.T) {
	mod, _ := ownershipModule(false)
	appP, appQ := astral.GenerateIdentity(), astral.GenerateIdentity()
	shared := astral.NewNonce()

	err := mod.ipcHandlers.Add(
		&IPCHandler{Identity: appP, Owner: appP, IPCToken: shared, Endpoint: deadEndpoint(t)},
		&IPCHandler{Identity: astral.GenerateIdentity(), Owner: appP, IPCToken: shared, Endpoint: "p-other"},
		&IPCHandler{Identity: appQ, Owner: appQ, IPCToken: shared, Endpoint: "q"},
	)
	if err != nil {
		t.Fatalf("add handlers: %v", err)
	}

	bindP := openBind(t, mod, appP, shared)
	bindQ := openBind(t, mod, appQ, shared)

	if err := routeTo(mod, appP); err == nil {
		t.Fatal("a query to a dead endpoint was routed")
	}

	if !bindP.ended(5 * time.Second) {
		t.Fatal("the failed dial left P's bind session open")
	}
	if bindQ.ended(200 * time.Millisecond) {
		t.Fatal("the failed dial of P's handler ended Q's bind session")
	}
	// note: "p-other" is unrouted only because its token is in the ended session
	if got := endpoints(mod.ipcHandlers.Clone()); !slices.Equal(got, []string{"q"}) {
		t.Fatalf("handlers left: %v; want only q", got)
	}
	if mod.bindSessions.find(appP, shared) != nil {
		t.Fatal("the ended session is still registered")
	}
}

// TestFailedDialOfAnOwnerlessHandlerEndsNoSession: a handler a token-less
// session registered has no session to end; its failed dial removes it and
// nothing else.
func TestFailedDialOfAnOwnerlessHandlerEndsNoSession(t *testing.T) {
	mod, _ := ownershipModule(false)
	app := astral.GenerateIdentity()
	token := astral.NewNonce()

	err := mod.ipcHandlers.Add(&IPCHandler{Identity: app, Owner: nil, IPCToken: token, Endpoint: deadEndpoint(t)})
	if err != nil {
		t.Fatalf("add handler: %v", err)
	}

	tokenless := openBind(t, mod, nil, token)
	owned := openBind(t, mod, app, token)

	if err := routeTo(mod, app); err == nil {
		t.Fatal("a query to a dead endpoint was routed")
	}

	if n := len(mod.ipcHandlers.Clone()); n != 0 {
		t.Fatalf("%d handlers left; want the failed one removed", n)
	}
	if tokenless.ended(200*time.Millisecond) || owned.ended(0) {
		t.Fatal("the failed dial of an owner-less handler ended a bind session")
	}
}

// spacerClock is a settable clock with a recorded timer for endSpacer.
type spacerClock struct {
	now   time.Time
	waits []time.Duration
	fns   []func()
}

func (c *spacerClock) install(mod *Module) {
	mod.sessionEnds.now = func() time.Time { return c.now }
	mod.sessionEnds.afterFunc = func(d time.Duration, fn func()) {
		c.waits = append(c.waits, d)
		c.fns = append(c.fns, fn)
	}
}

// countedBind registers a session of owner that bound token and counts its ends.
func countedBind(mod *Module, owner *astral.Identity, token astral.Nonce) (*appBind, *atomic.Int32) {
	var ends atomic.Int32
	s := &appBind{scope: bindScope{owner: owner}, cancel: func() { ends.Add(1) }}
	mod.bindToken(s, token)
	return s, &ends
}

// TestSessionEndsAreSpacedPerOwner: three failed dials of one owner within
// 30 s end its session once; the end requested inside the window runs when
// the window closes, and the next one after it runs at once.
func TestSessionEndsAreSpacedPerOwner(t *testing.T) {
	mod, _ := ownershipModule(false)
	clock := &spacerClock{now: time.Unix(1_000_000, 0)}
	clock.install(mod)
	app := astral.GenerateIdentity()
	token := astral.NewNonce()

	_, first := countedBind(mod, app, token)
	mod.endBindSession(app, token)
	if first.Load() != 1 {
		t.Fatalf("the first failed dial ended the session %d times; want 1", first.Load())
	}

	// the app reconnected and bound the same token from a new session
	_, second := countedBind(mod, app, token)
	for _, at := range []time.Duration{5 * time.Second, 10 * time.Second} {
		clock.now = clock.now.Add(at)
		mod.endBindSession(app, token)
	}
	if second.Load() != 0 {
		t.Fatalf("failed dials inside the 30 s window ended the new session %d times; want 0", second.Load())
	}
	if want := []time.Duration{25 * time.Second}; !slices.Equal(clock.waits, want) {
		t.Fatalf("scheduled ends wait %v; want %v", clock.waits, want)
	}

	clock.now = clock.now.Add(15 * time.Second)
	clock.fns[0]()
	if second.Load() != 1 {
		t.Fatalf("the deferred end ran %d times; want 1", second.Load())
	}

	_, third := countedBind(mod, app, token)
	clock.now = clock.now.Add(sessionEndSpacing)
	mod.endBindSession(app, token)
	if third.Load() != 1 || len(clock.fns) != 1 {
		t.Fatalf("a failed dial after the window: ends %d, scheduled %d; want 1 end at once", third.Load(), len(clock.fns))
	}
}

// TestSessionEndsOfOtherOwnersAreNotSpaced: the spacing is per owner.
func TestSessionEndsOfOtherOwnersAreNotSpaced(t *testing.T) {
	mod, _ := ownershipModule(false)
	clock := &spacerClock{now: time.Unix(1_000_000, 0)}
	clock.install(mod)
	appP, appQ := astral.GenerateIdentity(), astral.GenerateIdentity()
	token := astral.NewNonce()

	_, endsP := countedBind(mod, appP, token)
	_, endsQ := countedBind(mod, appQ, token)
	mod.endBindSession(appP, token)
	mod.endBindSession(appQ, token)

	if endsP.Load() != 1 || endsQ.Load() != 1 || len(clock.fns) != 0 {
		t.Fatalf("ends P %d, Q %d, scheduled %d; want both ended at once", endsP.Load(), endsQ.Load(), len(clock.fns))
	}
}

// TestEndedSessionKeepsALaterRegistration: the app re-registers the same
// endpoint and token as soon as its bind channel closes, before the old
// session's deferred cleanup runs. That cleanup leaves the new handler alone.
func TestEndedSessionKeepsALaterRegistration(t *testing.T) {
	mod, _ := ownershipModule(false)
	app := astral.GenerateIdentity()
	token := astral.NewNonce()

	err := mod.ipcHandlers.Add(&IPCHandler{Identity: app, Owner: app, IPCToken: token, Endpoint: "old"})
	if err != nil {
		t.Fatalf("add handler: %v", err)
	}

	reconnected := &appBind{scope: bindScope{owner: app}, cancel: func() {}}
	old := &appBind{scope: bindScope{owner: app}}
	var cancelled atomic.Bool
	old.cancel = func() {
		if !cancelled.CompareAndSwap(false, true) {
			return
		}
		// the app sees its bind channel close, registers again and binds again
		_ = mod.ipcHandlers.Add(&IPCHandler{Identity: app, Owner: app, IPCToken: token, Endpoint: "new"})
		mod.bindToken(reconnected, token)
	}
	mod.bindToken(old, token)

	mod.endBindSession(app, token)
	mod.finishBind(old) // OpBind's deferred cleanup

	if got := endpoints(mod.ipcHandlers.Clone()); !slices.Equal(got, []string{"new"}) {
		t.Fatalf("handlers left: %v; want only the re-registered one", got)
	}
	if mod.bindSessions.find(app, token) != reconnected {
		t.Fatal("the old session's cleanup dropped the new session's entry")
	}
}

// TestTokenBoundAfterTheEndIsRemoved: a BindMsg that reaches an ended session
// missed its cleanup, so the token's handlers are removed at once.
func TestTokenBoundAfterTheEndIsRemoved(t *testing.T) {
	mod, _ := ownershipModule(false)
	app := astral.GenerateIdentity()
	late := astral.NewNonce()

	s := &appBind{scope: bindScope{owner: app}, cancel: func() {}}
	mod.finishBind(s)

	err := mod.ipcHandlers.Add(&IPCHandler{Identity: app, Owner: app, IPCToken: late, Endpoint: "late"})
	if err != nil {
		t.Fatalf("add handler: %v", err)
	}
	mod.bindToken(s, late)

	if n := len(mod.ipcHandlers.Clone()); n != 0 {
		t.Fatalf("%d handlers left; want the late token's handler removed", n)
	}
	if mod.bindSessions.find(app, late) != nil {
		t.Fatal("an ended session registered a late token")
	}
}
