package apphost

import (
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// Fixtures for the ownership rules on apphost.bind and apphost.cancel. Both ops
// act on records another app owns, so both ask who is behind the query. The
// answer is the guest session's authenticated identity, seeded here the way the
// guest seeds it - on the en-route entry keyed by the query's own nonce.

// ownershipModule returns a module whose authority answers every question with
// verdict, and the recorder holding what it was asked.
func ownershipModule(verdict bool) (*Module, *recordingAuth) {
	authority := &recordingAuth{verdict: verdict}
	return &Module{Deps: Deps{Auth: authority}, log: log.New(nil)}, authority
}

// enRouteQuery seeds one en-route entry owned by owner - nil for a token-less
// session - and returns its nonce with the flag its cancel function sets.
func enRouteQuery(mod *Module, owner *astral.Identity) (astral.Nonce, *atomic.Bool) {
	var cancelled atomic.Bool

	q := query.New(owner, owner, "test.pending", nil)
	mod.enRoute.Set(q.Nonce, &queryEnRoute{
		query:  astral.Launch(q),
		cancel: func(error) { cancelled.Store(true) },
		owner:  owner,
	})

	return q.Nonce, &cancelled
}

// sessionQuery builds a query the way a guest session does. principal is the
// session that authenticated; caller is the identity the guest named, which
// differs when an app queries as an identity it holds a SudoAction for. A nil
// principal is a token-less session.
func sessionQuery(mod *Module, principal, caller *astral.Identity, queryString string) *astral.InFlightQuery {
	q := astral.Launch(query.New(caller, caller, queryString, nil))
	mod.enRoute.Set(q.Nonce, &queryEnRoute{query: q, cancel: func(error) {}, owner: principal})
	return q
}

// routeQuery dispatches one already-built query to one op.
func routeQuery(t *testing.T, fn any, q *astral.InFlightQuery, w *recordingWriter) error {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	_, err = op.RouteQuery(ctx, q, w)
	return err
}

// awaitOwnershipAnswer waits for the op to close the caller's end, which it does
// once it has answered.
func awaitOwnershipAnswer(t *testing.T, w *recordingWriter) {
	t.Helper()

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never closed the caller's connection")
	}
}

func endpoints(handlers []*IPCHandler) []string {
	var out []string
	for _, h := range handlers {
		out = append(out, h.Endpoint)
	}
	return out
}

// TestCancelOwnerEndsItsOwnQuery is the granted path: the session that launched
// the query cancels it, and no authority is asked to allow that.
func TestCancelOwnerEndsItsOwnQuery(t *testing.T) {
	mod, authority := ownershipModule(false)
	app := astral.GenerateIdentity()
	nonce, cancelled := enRouteQuery(mod, app)
	w := newRecordingWriter()

	q := sessionQuery(mod, app, app, "apphost.cancel?query_id="+nonce.String())
	if err := routeQuery(t, mod.OpCancel, q, w); err != nil {
		t.Fatalf("apphost.cancel refused the query's owner: %v", err)
	}
	awaitOwnershipAnswer(t, w)

	if !cancelled.Load() {
		t.Fatal("apphost.cancel left the owner's own query running")
	}

	if n := len(authority.recorded()); n != 0 {
		t.Fatalf("apphost.cancel made %d authorization calls for the owner; want none", n)
	}
}

// TestCancelOwnerEndsItsQueryFromASecondConnection: ownership is the identity,
// not the connection. The SDK cancels over a fresh connection that
// re-authenticates with the same token, and that must still reach the query.
func TestCancelOwnerEndsItsQueryFromASecondConnection(t *testing.T) {
	mod, _ := ownershipModule(false)
	app := astral.GenerateIdentity()
	nonce, cancelled := enRouteQuery(mod, app)
	w := newRecordingWriter()

	// why a nil caller: the SDK's cancel names no caller, so the query carries
	// only the session's token. Ownership must resolve from the session alone.
	q := sessionQuery(mod, app, nil, "apphost.cancel?query_id="+nonce.String())
	if err := routeQuery(t, mod.OpCancel, q, w); err != nil {
		t.Fatalf("apphost.cancel refused the owner on a second connection: %v", err)
	}
	awaitOwnershipAnswer(t, w)

	if !cancelled.Load() {
		t.Fatal("apphost.cancel left the owner's query running on a second connection")
	}
}

// TestCancelRefusesAnotherApp: a second app holding the exact nonce is refused,
// and the authority is asked about it by name.
func TestCancelRefusesAnotherApp(t *testing.T) {
	mod, authority := ownershipModule(false)
	owner, other := astral.GenerateIdentity(), astral.GenerateIdentity()
	nonce, cancelled := enRouteQuery(mod, owner)
	w := newRecordingWriter()

	q := sessionQuery(mod, other, other, "apphost.cancel?query_id="+nonce.String())
	if err := routeQuery(t, mod.OpCancel, q, w); err != nil {
		t.Fatalf("apphost.cancel errored instead of answering: %v", err)
	}
	awaitOwnershipAnswer(t, w)

	if cancelled.Load() {
		t.Fatal("apphost.cancel ended a query owned by another app")
	}

	actions := authority.recorded()
	if len(actions) != 1 {
		t.Fatalf("apphost.cancel made %d authorization calls; want exactly 1", len(actions))
	}

	action, ok := actions[0].(*auth.AdminManageAppsAction)
	if !ok {
		t.Fatalf("apphost.cancel named %q; want %q", actions[0].ObjectType(), auth.AdminManageAppsAction{}.ObjectType())
	}

	if !action.Actor().IsEqual(other) {
		t.Fatalf("apphost.cancel named actor %v; want the requesting session %v", action.Actor(), other)
	}
}

// TestCancelAnswersAForeignQueryAsMissing: a refused cancel and a cancel naming
// a nonce that is not en route answer alike, so a caller cannot probe which
// nonces other apps hold.
func TestCancelAnswersAForeignQueryAsMissing(t *testing.T) {
	mod, _ := ownershipModule(false)
	owner, other := astral.GenerateIdentity(), astral.GenerateIdentity()
	nonce, _ := enRouteQuery(mod, owner)

	foreign := newRecordingWriter()
	q := sessionQuery(mod, other, other, "apphost.cancel?query_id="+nonce.String())
	if err := routeQuery(t, mod.OpCancel, q, foreign); err != nil {
		t.Fatalf("apphost.cancel errored on a foreign query: %v", err)
	}
	awaitOwnershipAnswer(t, foreign)

	missing := newRecordingWriter()
	q = sessionQuery(mod, other, other, "apphost.cancel?query_id="+astral.NewNonce().String())
	if err := routeQuery(t, mod.OpCancel, q, missing); err != nil {
		t.Fatalf("apphost.cancel errored on a missing query: %v", err)
	}
	awaitOwnershipAnswer(t, missing)

	if foreign.written() != missing.written() {
		t.Fatalf("a foreign query answered %d bytes and a missing one %d; want one answer for both",
			foreign.written(), missing.written())
	}
}

// TestCancelRefusesATokenLessSession: an anonymous session owns nothing and is
// refused before the authority is asked. The core router's substitution of the
// node identity for a missing caller must not reach the administrative path.
func TestCancelRefusesATokenLessSession(t *testing.T) {
	mod, authority := ownershipModule(true)
	nonce, cancelled := enRouteQuery(mod, astral.GenerateIdentity())
	w := newRecordingWriter()

	q := sessionQuery(mod, nil, nil, "apphost.cancel?query_id="+nonce.String())
	if err := routeQuery(t, mod.OpCancel, q, w); err != nil {
		t.Fatalf("apphost.cancel errored instead of answering: %v", err)
	}
	awaitOwnershipAnswer(t, w)

	if cancelled.Load() {
		t.Fatal("a token-less session cancelled an authenticated app's query")
	}

	if n := len(authority.recorded()); n != 0 {
		t.Fatalf("apphost.cancel asked the authority %d times about a token-less session; want none", n)
	}
}

// TestCancelKeepsAnUnownedQueryCancellable holds what a token-less app had
// before ownership: a query launched without a token records no owner, and any
// local session still cancels it.
func TestCancelKeepsAnUnownedQueryCancellable(t *testing.T) {
	mod, _ := ownershipModule(false)
	nonce, cancelled := enRouteQuery(mod, nil)
	w := newRecordingWriter()

	q := sessionQuery(mod, nil, nil, "apphost.cancel?query_id="+nonce.String())
	if err := routeQuery(t, mod.OpCancel, q, w); err != nil {
		t.Fatalf("apphost.cancel refused a token-less session its own query: %v", err)
	}
	awaitOwnershipAnswer(t, w)

	if !cancelled.Load() {
		t.Fatal("apphost.cancel left an unowned query running")
	}
}

// TestCancelRefusesAQueryOffALink: an unowned entry is exactly the one ownership
// leaves cancellable by any caller, so the origin refusal is what keeps a caller
// off a link from reaching it. The authority is never asked, because the origin
// refusal runs first.
func TestCancelRefusesAQueryOffALink(t *testing.T) {
	mod, authority := ownershipModule(true)
	nonce, cancelled := enRouteQuery(mod, nil)
	w := newRecordingWriter()

	q := sessionQuery(mod, nil, astral.GenerateIdentity(), "apphost.cancel?query_id="+nonce.String())
	q.Extra.Set("origin", astral.OriginNetwork)

	err := routeQuery(t, mod.OpCancel, q, w)

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("apphost.cancel answered a query off a link: got err %v, want a rejection", err)
	}

	if cancelled.Load() {
		t.Fatal("a caller off a link cancelled an unowned query")
	}

	if n := w.written(); n != 0 {
		t.Fatalf("apphost.cancel wrote %d bytes to a query off a link; want none", n)
	}

	if n := len(authority.recorded()); n != 0 {
		t.Fatalf("apphost.cancel asked the authority %d times about a query off a link; want none", n)
	}
}

// TestCancelAdminEndsAnotherAppsQuery covers the administrative override: an
// identity holding AdminManageApps ends an app's query without holding that
// app's token.
func TestCancelAdminEndsAnotherAppsQuery(t *testing.T) {
	mod, authority := ownershipModule(true)
	owner, admin := astral.GenerateIdentity(), astral.GenerateIdentity()
	nonce, cancelled := enRouteQuery(mod, owner)
	w := newRecordingWriter()

	q := sessionQuery(mod, admin, admin, "apphost.cancel?query_id="+nonce.String())
	if err := routeQuery(t, mod.OpCancel, q, w); err != nil {
		t.Fatalf("apphost.cancel refused an administrator: %v", err)
	}
	awaitOwnershipAnswer(t, w)

	if !cancelled.Load() {
		t.Fatal("an administrator's cancel left the query running")
	}

	actions := authority.recorded()
	if len(actions) != 1 {
		t.Fatalf("apphost.cancel made %d authorization calls; want exactly 1", len(actions))
	}

	if _, ok := actions[0].(*auth.AdminManageAppsAction); !ok {
		t.Fatalf("apphost.cancel named %q; want %q", actions[0].ObjectType(), auth.AdminManageAppsAction{}.ObjectType())
	}
}

// TestEnRouteNonceCollisionKeepsTheFirstOwner: sig.Map.Set refuses to overwrite,
// so a second query choosing a nonce already en route never takes over the
// entry. It becomes uncancellable; it never adopts another app's ownership.
func TestEnRouteNonceCollisionKeepsTheFirstOwner(t *testing.T) {
	mod, _ := ownershipModule(false)
	first, second := astral.GenerateIdentity(), astral.GenerateIdentity()
	nonce, cancelled := enRouteQuery(mod, first)

	collision := query.New(second, second, "test.colliding", nil)
	collision.Nonce = nonce
	mod.enRoute.Set(nonce, &queryEnRoute{
		query:  astral.Launch(collision),
		cancel: func(error) {},
		owner:  second,
	})

	entry, found := mod.enRoute.Get(nonce)
	if !found {
		t.Fatal("the en-route entry disappeared on a nonce collision")
	}
	if !entry.owner.IsEqual(first) {
		t.Fatalf("a colliding query took the entry: its owner is %v; want the first owner %v", entry.owner, first)
	}

	w := newRecordingWriter()
	q := sessionQuery(mod, second, second, "apphost.cancel?query_id="+nonce.String())
	if err := routeQuery(t, mod.OpCancel, q, w); err != nil {
		t.Fatalf("apphost.cancel errored instead of answering: %v", err)
	}
	awaitOwnershipAnswer(t, w)

	if cancelled.Load() {
		t.Fatal("the colliding query's owner cancelled the first owner's query")
	}
}

// TestRemoveHandlersByTokenMatchesOwnerAndToken is the removal helper's table: a
// token alone never reaches another owner's handler.
func TestRemoveHandlersByTokenMatchesOwnerAndToken(t *testing.T) {
	shared, other := astral.NewNonce(), astral.NewNonce()
	appA, appB := astral.GenerateIdentity(), astral.GenerateIdentity()

	registered := func() []*IPCHandler {
		return []*IPCHandler{
			{Identity: appA, Owner: appA, IPCToken: shared, Endpoint: "a"},
			{Identity: appB, Owner: appB, IPCToken: shared, Endpoint: "b"},
			{Identity: appA, Owner: nil, IPCToken: shared, Endpoint: "unowned"},
			{Identity: appA, Owner: appA, IPCToken: other, Endpoint: "other-token"},
		}
	}

	tests := []struct {
		name  string
		scope bindScope
		kept  []string
	}{
		{"the binder reaches its own", bindScope{owner: appA}, []string{"b", "unowned", "other-token"}},
		{"a token-less binder reaches the unowned", bindScope{}, []string{"a", "b", "other-token"}},
		{"an administrator reaches every owner", bindScope{owner: appA, anyOwner: true}, []string{"other-token"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mod, _ := ownershipModule(false)
			if err := mod.ipcHandlers.Add(registered()...); err != nil {
				t.Fatalf("add handlers: %v", err)
			}

			if err := mod.removeHandlersByToken(test.scope, shared); err != nil {
				t.Fatalf("remove handlers: %v", err)
			}

			if got := endpoints(mod.ipcHandlers.Clone()); !slices.Equal(got, test.kept) {
				t.Fatalf("handlers left: %v; want %v", got, test.kept)
			}
		})
	}
}

// bindSession runs one apphost.bind session as principal, sends one BindMsg
// naming token, and returns once the handler endpoints left are want, or once
// the session's deadline elapses.
func bindSession(t *testing.T, mod *Module, principal *astral.Identity, token astral.Nonce, want []string) {
	t.Helper()

	op, err := routing.NewOp(mod.OpBind)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	q := sessionQuery(mod, principal, principal, "apphost.bind")
	w := newRecordingWriter()

	conn, err := op.RouteQuery(ctx, q, w)
	if err != nil {
		t.Fatalf("apphost.bind refused %v: %v", principal, err)
	}

	if err = channel.NewSender(conn).Send(&apphost.BindMsg{Token: token}); err != nil {
		t.Fatalf("send bind message: %v", err)
	}
	conn.Close()

	awaitOwnershipAnswer(t, w)

	// why: awaitOwnershipAnswer reports that the caller's end closed, which
	// astral-go's routing.Conn.Read does from inside the op's own read on any
	// read error (lib/routing/conn.go:30-37), strictly before OpBind returns
	// from ch.Switch and runs its deferred cleanup actions (op_bind.go:37-43).
	// Wait for the actions to land, then let the caller's assertion report a
	// real leak.
	for !slices.Equal(endpoints(mod.ipcHandlers.Clone()), want) {
		if ctx.Err() != nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// TestBindRemovesOnlyTheBindersHandlers: two apps register under the same token
// value, and a closing bind reaches only its own.
func TestBindRemovesOnlyTheBindersHandlers(t *testing.T) {
	mod, _ := ownershipModule(false)
	appA, appB := astral.GenerateIdentity(), astral.GenerateIdentity()
	shared := astral.NewNonce()

	err := mod.ipcHandlers.Add(
		&IPCHandler{Identity: appA, Owner: appA, IPCToken: shared, Endpoint: "a"},
		&IPCHandler{Identity: appB, Owner: appB, IPCToken: shared, Endpoint: "b"},
	)
	if err != nil {
		t.Fatalf("add handlers: %v", err)
	}

	bindSession(t, mod, appA, shared, []string{"b"})

	if got := endpoints(mod.ipcHandlers.Clone()); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("after the bind closed, the handlers left are %v; want only b", got)
	}
}

// TestBindAdminRemovesAnotherAppsHandlers: an administrator's bind removes by
// token alone, which is how an operator clears a stuck handler.
func TestBindAdminRemovesAnotherAppsHandlers(t *testing.T) {
	mod, _ := ownershipModule(true)
	app, admin := astral.GenerateIdentity(), astral.GenerateIdentity()
	token := astral.NewNonce()

	err := mod.ipcHandlers.Add(&IPCHandler{Identity: app, Owner: app, IPCToken: token, Endpoint: "stuck"})
	if err != nil {
		t.Fatalf("add handler: %v", err)
	}

	bindSession(t, mod, admin, token, nil)

	if n := len(mod.ipcHandlers.Clone()); n != 0 {
		t.Fatalf("an administrator's bind left %d handlers; want none", n)
	}
}

// TestBindKeepsTheNetworkRefusal holds the refusal that predates ownership: a
// bind off a link is rejected before any session is resolved.
func TestBindKeepsTheNetworkRefusal(t *testing.T) {
	mod, authority := ownershipModule(true)
	w := newRecordingWriter()

	// note: reuses the ServeApps fixture's network route - one query stamped the
	// way mod/nodes stamps one.
	err := routeServeAppsFromNetwork(t, mod.OpBind, astral.GenerateIdentity(), "apphost.bind", w)

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("apphost.bind answered a query off a link: got err %v, want a rejection", err)
	}

	if n := len(authority.recorded()); n != 0 {
		t.Fatalf("apphost.bind asked the authority %d times for a query off a link; want none", n)
	}
}

// TestRegisterHandlerRecordsTheSessionOwner: the op path records the session
// behind the query, never the identity the query names as its caller.
func TestRegisterHandlerRecordsTheSessionOwner(t *testing.T) {
	mod, _ := ownershipModule(true)
	session, named := astral.GenerateIdentity(), astral.GenerateIdentity()
	w := newRecordingWriter()

	q := sessionQuery(mod, session, named,
		"apphost.register_handler?endpoint=tcp:127.0.0.1:9001&token="+astral.NewNonce().String())
	if err := routeQuery(t, mod.OpRegisterHandler, q, w); err != nil {
		t.Fatalf("apphost.register_handler refused an authorized session: %v", err)
	}
	awaitOwnershipAnswer(t, w)

	handlers := mod.ipcHandlers.Clone()
	if len(handlers) != 1 {
		t.Fatalf("apphost.register_handler installed %d handlers; want 1", len(handlers))
	}

	if !handlers[0].Owner.IsEqual(session) {
		t.Fatalf("the handler records owner %v; want the session %v", handlers[0].Owner, session)
	}

	if !handlers[0].Identity.IsEqual(named) {
		t.Fatalf("the handler answers for %v; want the named caller %v", handlers[0].Identity, named)
	}
}
