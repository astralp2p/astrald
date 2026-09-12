package mcp

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// adminManageAppsOp is one row of the AdminManageApps surface in mod/mcp: the op
// and a query that binds its required arguments.
type adminManageAppsOp struct {
	name string
	op   func(*Module) any
	args string
}

func adminManageAppsOps() []adminManageAppsOp {
	return []adminManageAppsOp{
		{"mcp.create_agent", func(m *Module) any { return m.OpCreateAgent }, "?alias=scout"},
		{"mcp.list_agents", func(m *Module) any { return m.OpListAgents }, ""},
		{"mcp.delete_agent", func(m *Module) any { return m.OpDeleteAgent }, "?id=scout"},
	}
}

// TestAdminManageAppsRefusesCallerWithoutPermits is the coverage measure for the
// AdminManageApps action in mod/mcp: every agent-credential op must ask before
// it reads or changes an agent record, and must reject when the answer is no.
//
// The module is a bare struct — no database, no directory, no apphost. An op
// that reached past its authorization check would panic on a nil field, so
// "touches no agent" is enforced by construction as well as by the byte count.
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

// TestAdminManageAppsListsAgentsToAuthorizedCaller: the check refuses only what
// the authority refuses. An authorized caller receives the stream.
func TestAdminManageAppsListsAgentsToAuthorizedCaller(t *testing.T) {
	mod := testMessageModule(t)
	authority := &recordingAuth{verdict: true}
	mod.Auth = authority
	caller := astral.GenerateIdentity()

	if err := mod.db.CreateAgent(&dbAgent{Identity: astral.GenerateIdentity(), Token: "token-a"}); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	w := newRecordingWriter()
	if err := route(t, mod.OpListAgents, caller, "mcp.list_agents", w); err != nil {
		t.Fatalf("mcp.list_agents refused an authorized caller: %v", err)
	}

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("mcp.list_agents never closed the caller's connection")
	}

	if w.written() == 0 {
		t.Fatal("mcp.list_agents answered an authorized caller nothing")
	}

	checkAdminManageAppsAsked(t, "mcp.list_agents", authority, caller)
}

// TestAdminManageAppsKeepsNetworkRefusal: every agent-credential op refuses a
// query off a link whatever the authority answers.
func TestAdminManageAppsKeepsNetworkRefusal(t *testing.T) {
	for _, op := range adminManageAppsOps() {
		t.Run(op.name, func(t *testing.T) {
			mod := testMessageModule(t)
			mod.Auth = &recordingAuth{verdict: true}
			w := newRecordingWriter()

			err := routeAdminManageAppsFromNetwork(t, op.op(mod), astral.GenerateIdentity(), op.name+op.args, w)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("%s answered a query off a link: got err %v, want a rejection", op.name, err)
			}
			if n := w.written(); n != 0 {
				t.Fatalf("%s wrote %d bytes to a query off a link; want none", op.name, n)
			}
		})
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
