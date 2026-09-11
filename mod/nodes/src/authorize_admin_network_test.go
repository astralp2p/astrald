package nodes

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// adminNetworkNonce is a well-formed nonce64; no op in the refusal table gets
// far enough to look it up.
const adminNetworkNonce = "7c1a93b50f2e4d18"

// adminNetworkOp is one row of the AdminNetwork surface in mod/nodes: the op and
// a query that binds its required arguments.
type adminNetworkOp struct {
	name string
	op   func(*Module) any
	args string
}

func adminNetworkOps(id *astral.Identity) []adminNetworkOp {
	return []adminNetworkOp{
		{"nodes.links", func(m *Module) any { return m.OpLinks }, ""},
		{"nodes.sessions", func(m *Module) any { return m.OpSessions }, ""},
		{"nodes.resolve_endpoints", func(m *Module) any { return m.OpResolveEndpoints }, "?id=anything"},
		{"nodes.new_link", func(m *Module) any { return m.OpNewLink }, "?target=anything"},
		{"nodes.add_endpoint", func(m *Module) any { return m.OpAddEndpoint }, "?id=" + id.String() + "&endpoint=tcp:192.0.2.1:1791"},
		{"nodes.close_link", func(m *Module) any { return m.OpCloseLink }, "?id=" + adminNetworkNonce},
		{"nodes.migrate_session", func(m *Module) any { return m.OpMigrateSession }, "?session_id=" + adminNetworkNonce + "&link_id=" + adminNetworkNonce},
	}
}

// TestAdminNetworkRefusesCallerWithoutPermits is the coverage measure for the
// AdminNetwork action in mod/nodes: every guarded op asks before it acts and
// rejects when the answer is no.
//
// note: the module is a bare struct with no link pool, directory, exonet, or
// scheduler. An op that reached past its check would panic on a nil field.
func TestAdminNetworkRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range adminNetworkOps(caller) {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := &Module{Deps: Deps{Auth: authority}}
			w := newRecordingWriter()

			err := route(t, op.op(mod), caller, op.name+op.args, w)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("answered a caller holding no permits: got err %v, want a rejection", err)
			}

			if n := w.written(); n != 0 {
				t.Fatalf("wrote %d bytes to a refused caller; want none", n)
			}

			requireAdminNetworkAsked(t, authority, caller)
		})
	}
}

// TestAdminNetworkAdmitsAuthorizedCaller shows the check passes a granted
// caller through: nodes.links accepts and streams the link table.
func TestAdminNetworkAdmitsAuthorizedCaller(t *testing.T) {
	caller := astral.GenerateIdentity()
	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority}, linkPool: &LinkPool{}}
	w := newRecordingWriter()

	err := route(t, mod.OpLinks, caller, "nodes.links", w)
	if err != nil {
		t.Fatalf("refused an authorized caller: %v", err)
	}

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never closed the caller's connection")
	}

	if w.written() == 0 {
		t.Fatal("wrote nothing to an authorized caller")
	}

	requireAdminNetworkAsked(t, authority, caller)
}

// requireAdminNetworkAsked asserts exactly one AdminNetwork question, naming the
// caller as the actor.
func requireAdminNetworkAsked(t *testing.T, authority *recordingAuth, caller *astral.Identity) {
	t.Helper()

	actions := authority.recorded()
	if len(actions) != 1 {
		t.Fatalf("made %d authorization calls; want exactly 1", len(actions))
	}

	action, ok := actions[0].(*auth.AdminNetworkAction)
	if !ok {
		t.Fatalf("named %q; want %q", actions[0].ObjectType(), (&auth.AdminNetworkAction{}).ObjectType())
	}

	if !action.Actor().IsEqual(caller) {
		t.Fatalf("named actor %v; want the caller %v", action.Actor(), caller)
	}
}
