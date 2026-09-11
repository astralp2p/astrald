package nat

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// adminNetworkOp is one row of the AdminNetwork surface in mod/nat: the op and a
// query that binds its required arguments.
type adminNetworkOp struct {
	name string
	op   func(*Module) any
	args string
}

func adminNetworkOps() []adminNetworkOp {
	return []adminNetworkOp{
		{"nat.list_holes", func(m *Module) any { return m.OpListHoles }, ""},
		{"nat.punch", func(m *Module) any { return m.OpPunch }, "?target=anything"},
		{"nat.node_punch", func(m *Module) any { return m.OpNodePunch }, ""},
		{"nat.node_consume_hole", func(m *Module) any { return m.OpNodeConsumeHole }, "?pair=7c1a93b50f2e4d18"},
		{"nat.set_enabled", func(m *Module) any { return m.OpSetEnabled }, "?arg=true"},
	}
}

// TestAdminNetworkRefusesCallerWithoutPermits is the coverage measure for the
// AdminNetwork action in mod/nat: every guarded op asks before it acts and
// rejects when the answer is no.
//
// note: the module is a bare struct with no hole pool, directory, or settings.
// An op that reached past its check would panic on a nil field.
func TestAdminNetworkRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range adminNetworkOps() {
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
// caller through: nat.set_enabled accepts, stores the setting, and
// acknowledges.
func TestAdminNetworkAdmitsAuthorizedCaller(t *testing.T) {
	caller := astral.GenerateIdentity()
	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority}}
	mod.settings.Enabled = &tree.Value[*astral.Bool]{}
	w := newRecordingWriter()

	err := route(t, mod.OpSetEnabled, caller, "nat.set_enabled?arg=true", w)
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

	if v := mod.settings.Enabled.Get(); v == nil || !bool(*v) {
		t.Fatalf("stored %v; want the enabled setting true", v)
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
