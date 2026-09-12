package nearby

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// adminNetworkOp is one row of the AdminNetwork surface in mod/nearby: the op
// and a query that binds its required arguments.
type adminNetworkOp struct {
	name string
	op   func(*Module) any
	args string
}

func adminNetworkOps() []adminNetworkOp {
	return []adminNetworkOp{
		{"nearby.list", func(m *Module) any { return m.OpList }, ""},
		{"nearby.broadcast", func(m *Module) any { return m.OpBroadcast }, ""},
	}
}

// TestAdminNetworkRefusesCallerWithoutPermits is the coverage measure for the
// AdminNetwork action in mod/nearby: every guarded op asks before it acts and
// rejects when the answer is no.
//
// note: the module is a bare struct. An op that reached past its check would
// accept the query and answer it, or panic on a nil field.
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
// caller through: nearby.list accepts and streams the cache.
func TestAdminNetworkAdmitsAuthorizedCaller(t *testing.T) {
	caller := astral.GenerateIdentity()
	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority}}
	w := newRecordingWriter()

	err := route(t, mod.OpList, caller, "nearby.list", w)
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
