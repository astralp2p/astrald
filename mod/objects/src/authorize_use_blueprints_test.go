package objects

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// useBlueprintsOps lists every blueprint op with a query that binds its
// required arguments.
func useBlueprintsOps() map[string]struct {
	op   func(*Module) any
	args string
} {
	return map[string]struct {
		op   func(*Module) any
		args string
	}{
		"objects.blueprints":         {func(m *Module) any { return m.OpBlueprints }, ""},
		"objects.get_blueprint":      {func(m *Module) any { return m.OpGetBlueprint }, "?type=" + (&astral.Blob{}).ObjectType()},
		"objects.register_blueprint": {func(m *Module) any { return m.OpRegisterBlueprint }, ""},
	}
}

// TestUseBlueprintsRefusesCallerWithoutPermits: every blueprint op asks
// UseBlueprints, and only it, before it acts, and rejects when the answer is no.
func TestUseBlueprintsRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for name, op := range useBlueprintsOps() {
		t.Run(name, func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := &Module{Deps: Deps{Auth: authority}}
			w := newRecordingWriter()

			err := route(t, op.op(mod), caller, name+op.args, w)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("%s answered a caller holding no permits: got err %v, want a rejection", name, err)
			}
			if n := w.written(); n != 0 {
				t.Fatalf("%s wrote %d bytes to a refused caller; want none", name, n)
			}

			actions := authority.recorded()
			if len(actions) != 1 {
				t.Fatalf("%s made %d authorization calls; want exactly 1", name, len(actions))
			}
			action, ok := actions[0].(*auth.UseBlueprintsAction)
			if !ok {
				t.Fatalf("%s named %q; want %q", name, actions[0].ObjectType(), auth.UseBlueprintsAction{}.ObjectType())
			}
			if !action.Actor().IsEqual(caller) {
				t.Fatalf("%s named actor %v; want the caller %v", name, action.Actor(), caller)
			}
		})
	}
}

// TestUseBlueprintsAllowsGrantedCaller covers the granted path once.
// objects.blueprints reads no repository, so a bare module answers it in full.
func TestUseBlueprintsAllowsGrantedCaller(t *testing.T) {
	mod := &Module{Deps: Deps{Auth: &recordingAuth{verdict: true}}}
	w := newRecordingWriter()

	if err := route(t, mod.OpBlueprints, astral.GenerateIdentity(), "objects.blueprints", w); err != nil {
		t.Fatalf("granted caller was refused: %v", err)
	}

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("objects.blueprints did not finish answering a granted caller")
	}
	if w.written() == 0 {
		t.Fatal("objects.blueprints wrote nothing to a granted caller")
	}
}
