package dir

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// seeNodeStateOp is one row of the SeeNodeState surface in mod/dir: the op and a
// query that binds its required arguments.
type seeNodeStateOp struct {
	name string
	op   func(*Module) any
	args string
}

func seeNodeStateOps() []seeNodeStateOp {
	return []seeNodeStateOp{
		{"dir.alias_map", func(m *Module) any { return m.OpAliasMap }, ""},
		{"dir.filters", func(m *Module) any { return m.OpFilters }, ""},
		{"dir.apply_filters", func(m *Module) any { return m.OpApplyFilters }, "?filters=all"},
	}
}

// TestSeeNodeStateRefusesCallerWithoutPermits is the coverage measure for the
// SeeNodeState action in mod/dir: every guarded read must ask before it acts,
// and must reject when the answer is no.
//
// note: the module is a bare struct with no database, so dir.alias_map past its
// authorization check would panic on the nil database.
// note: dir.filters and dir.apply_filters answer from empty filter sets past the
// check, which the byte count catches.
func TestSeeNodeStateRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range seeNodeStateOps() {
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

			actions := authority.recorded()
			if len(actions) != 1 {
				t.Fatalf("%s made %d authorization calls; want exactly 1", op.name, len(actions))
			}

			action, ok := actions[0].(*auth.SeeNodeStateAction)
			if !ok {
				t.Fatalf("%s named %q; want %q", op.name, actions[0].ObjectType(), (&auth.SeeNodeStateAction{}).ObjectType())
			}

			if !action.Actor().IsEqual(caller) {
				t.Fatalf("%s named actor %v; want the caller %v", op.name, action.Actor(), caller)
			}
		})
	}
}

// TestSeeNodeStateAnswersHolder is the allowed path: a caller holding the action
// receives the filter names.
func TestSeeNodeStateAnswersHolder(t *testing.T) {
	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority}}
	mod.filters.Set("all", func(*astral.Identity) bool { return true })
	w := newRecordingWriter()

	if err := route(t, mod.OpFilters, astral.GenerateIdentity(), "dir.filters", w); err != nil {
		t.Fatalf("dir.filters refused a caller holding the action: %v", err)
	}

	select {
	case <-w.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("dir.filters did not finish for a caller holding the action")
	}

	if w.written() == 0 {
		t.Fatal("dir.filters wrote nothing to a caller holding the action")
	}

	if n := len(authority.recorded()); n != 1 {
		t.Fatalf("dir.filters made %d authorization calls; want exactly 1", n)
	}
}
