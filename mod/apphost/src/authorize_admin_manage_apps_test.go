package apphost

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// adminManageAppsOp is one row of the AdminManageApps surface in mod/apphost: the
// op and a query that binds its required arguments.
type adminManageAppsOp struct {
	name string
	op   func(*Module) any
	args string
}

func adminManageAppsOps() []adminManageAppsOp {
	id := astral.GenerateIdentity()
	return []adminManageAppsOp{
		{"apphost.create_token", func(m *Module) any { return m.OpCreateToken }, "?id=" + id.String()},
		{"apphost.list_tokens", func(m *Module) any { return m.OpListTokens }, ""},
		{"apphost.delete_token", func(m *Module) any { return m.OpDeleteToken }, "?token=k7m2q5x9r3v4n8p1"},
	}
}

// TestAdminManageAppsRefusesCallerWithoutPermits is the coverage measure for the
// AdminManageApps action in mod/apphost: every token op must ask before it reads
// or changes a token, and must reject when the answer is no.
//
// The module is a bare struct — no database, no logger. An op that reached past
// its authorization check would panic on a nil field, so "touches no token" is
// enforced by construction as well as by the byte count.
func TestAdminManageAppsRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range adminManageAppsOps() {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := &Module{Deps: Deps{Auth: authority}}
			w := newRecordingWriter()

			err := route(t, op.op(mod), caller, op.name+op.args, w)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("%s answered a caller holding no permits: got err %v, want a rejection", op.name, err)
			}

			if n := w.written(); n != 0 {
				t.Fatalf("%s wrote %d bytes to a refused caller; want none", op.name, n)
			}

			checkAdminManageAppsAsked(t, op.name, authority, caller)
		})
	}
}

// TestAdminManageAppsIssuesTokenToAuthorizedCaller: the check refuses only what
// the authority refuses. An authorized caller receives the token, and the node
// holds it.
func TestAdminManageAppsIssuesTokenToAuthorizedCaller(t *testing.T) {
	mod := testTokenModule(t)
	mod.log = log.New(nil)
	authority := &recordingAuth{verdict: true}
	mod.Auth = authority
	caller, holder := astral.GenerateIdentity(), astral.GenerateIdentity()
	w := newRecordingWriter()

	err := route(t, mod.OpCreateToken, caller, "apphost.create_token?id="+holder.String(), w)
	if err != nil {
		t.Fatalf("apphost.create_token refused an authorized caller: %v", err)
	}
	waitAdminManageAppsAnswer(t, w)

	if w.written() == 0 {
		t.Fatal("apphost.create_token answered an authorized caller nothing")
	}

	tokens, err := mod.ListAccessTokens()
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(tokens) != 1 || !tokens[0].Identity.IsEqual(holder) {
		t.Fatalf("the node holds %d tokens after one was issued to %v; want that one", len(tokens), holder)
	}

	checkAdminManageAppsAsked(t, "apphost.create_token", authority, caller)
}

// TestAdminManageAppsKeepsNetworkRefusal: apphost.delete_token refuses a query
// off a link whatever the authority answers, and the token keeps authenticating.
func TestAdminManageAppsKeepsNetworkRefusal(t *testing.T) {
	mod := testTokenModule(t)
	mod.log = log.New(nil)
	mod.Auth = &recordingAuth{verdict: true}

	token, err := mod.CreateAccessToken(astral.GenerateIdentity(), astral.Duration(time.Hour))
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	w := newRecordingWriter()
	err = routeAdminManageAppsFromNetwork(t, mod.OpDeleteToken, astral.GenerateIdentity(), "apphost.delete_token?token="+string(token.Token), w)

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("apphost.delete_token answered a query off a link: got err %v, want a rejection", err)
	}
	if n := w.written(); n != 0 {
		t.Fatalf("apphost.delete_token wrote %d bytes to a query off a link; want none", n)
	}
	if _, err = mod.AuthenticateToken(string(token.Token)); err != nil {
		t.Fatalf("the token stopped authenticating after a refused delete: %v", err)
	}
}

// checkAdminManageAppsAsked asserts exactly one authorization call, naming
// AdminManageApps with the query's caller as the actor.
func checkAdminManageAppsAsked(t *testing.T, name string, authority *recordingAuth, caller *astral.Identity) {
	t.Helper()

	actions := authority.recorded()
	if len(actions) != 1 {
		t.Fatalf("%s made %d authorization calls; want exactly 1", name, len(actions))
	}

	action, ok := actions[0].(*auth.AdminManageAppsAction)
	if !ok {
		t.Fatalf("%s named %q; want %q", name, actions[0].ObjectType(), (&auth.AdminManageAppsAction{}).ObjectType())
	}

	if !action.Actor().IsEqual(caller) {
		t.Fatalf("%s named actor %v; want the caller %v", name, action.Actor(), caller)
	}
}

// waitAdminManageAppsAnswer waits for the op to close the caller's end, which it
// does once it has answered.
func waitAdminManageAppsAnswer(t *testing.T, w *recordingWriter) {
	t.Helper()

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never closed the caller's connection")
	}
}

// routeAdminManageAppsFromNetwork dispatches one query stamped as arriving over a
// link, the way mod/nodes stamps one, and returns the router's verdict.
func routeAdminManageAppsFromNetwork(t *testing.T, fn any, caller *astral.Identity, queryString string, w io.WriteCloser) error {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	q := astral.Launch(query.New(caller, caller, queryString, nil))
	q.Extra.Set("origin", astral.OriginNetwork)

	_, err = op.RouteQuery(ctx, q, w)
	return err
}
