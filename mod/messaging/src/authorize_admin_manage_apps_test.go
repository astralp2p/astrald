package messaging

import (
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// adminManageAppsOp is one row of the AdminManageApps surface in mod/messaging:
// the op and a query that binds its required arguments.
type adminManageAppsOp struct {
	name string
	op   func(*Module) any
	args string
}

func adminManageAppsOps() []adminManageAppsOp {
	return []adminManageAppsOp{
		{"messaging.create_mailbox", func(m *Module) any { return m.OpCreateMailbox }, "?alias=scout"},
		{"messaging.delete_mailbox", func(m *Module) any { return m.OpDeleteMailbox }, "?identity=scout"},
	}
}

// TestAdminManageAppsRefusesCallerWithoutPermits is the coverage measure for the
// AdminManageApps action in mod/messaging: every identity-credential op must ask
// before it reads or changes a participant, and must reject when the answer is
// no.
//
// The module is a bare struct — no database, no directory, no apphost. An op
// that reached past its authorization check would panic on a nil field, so
// "touches no participant" is enforced by construction as well as by the byte
// count.
func TestAdminManageAppsRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range adminManageAppsOps() {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := &Module{Deps: Deps{Auth: authority}}
			w := newRecordingWriter()

			err := route(t, op.op(mod), caller, op.name+op.args, w)

			if err != nil {
				t.Fatalf("route: %v", err)
			}
			if got, want := w.refusal(t), auth.ErrNotPermitted.Error(); got != want {
				t.Fatalf("answered %q; want %q", got, want)
			}

			checkAdminManageAppsAsked(t, op.name, authority, caller)
		})
	}
}

// TestAdminManageAppsDeletesForAuthorizedCaller: the check refuses only what
// the authority refuses. An authorized caller receives the answer.
func TestAdminManageAppsDeletesForAuthorizedCaller(t *testing.T) {
	mod, _, dir := testIdentityModule(t)
	dir.aliases["scout"] = hostedParticipant(t, mod)

	authority := &recordingAuth{verdict: true}
	mod.Auth = authority
	caller := astral.GenerateIdentity()

	w := newRecordingWriter()
	if err := route(t, mod.OpDeleteMailbox, caller, "messaging.delete_mailbox?identity=scout", w); err != nil {
		t.Fatalf("messaging.delete_mailbox refused an authorized caller: %v", err)
	}

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("messaging.delete_mailbox never closed the caller's connection")
	}

	if w.written() == 0 {
		t.Fatal("messaging.delete_mailbox answered an authorized caller nothing")
	}

	checkAdminManageAppsAsked(t, "messaging.delete_mailbox", authority, caller)
}

// TestAdminManageAppsKeepsOriginRefusal: every identity-credential op refuses a
// query off a link or from an MCP agent, whatever the authority answers.
func TestAdminManageAppsKeepsOriginRefusal(t *testing.T) {
	for _, origin := range []string{astral.OriginNetwork, astral.OriginMCP} {
		for _, op := range adminManageAppsOps() {
			t.Run(origin+"/"+op.name, func(t *testing.T) {
				mod := testMessagingModule(t)
				authority := &recordingAuth{verdict: true}
				mod.Auth = authority
				w := newRecordingWriter()

				q := originQuery(astral.GenerateIdentity(), op.name+op.args, origin)
				err := routeQuery(t, op.op(mod), q, w)

				if err != nil {
					t.Fatalf("route: %v", err)
				}
				if got, want := w.refusal(t), astral.NewErrOriginNotAllowed(astral.OriginNetwork).Error(); got != want {
					t.Fatalf("answered %q; want %q", got, want)
				}
				if n := len(authority.recorded()); n != 0 {
					t.Fatalf("%s asked the authority %d times about a query from %v; want none", op.name, n, origin)
				}
			})
		}
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

// originQuery is a query stamped with origin, the way mod/nodes stamps one off
// a link and mod/mcp stamps one an agent puts.
func originQuery(caller *astral.Identity, queryString, origin string) *astral.InFlightQuery {
	q := astral.Launch(query.New(caller, caller, queryString, nil))
	q.Extra.Set("origin", origin)
	return q
}

// routeQuery dispatches one query to one op and returns the router's verdict
// once the op has resolved it.
func routeQuery(t *testing.T, fn any, q *astral.InFlightQuery, w io.WriteCloser) error {
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
