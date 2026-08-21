package objects

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// adminOp is one row of the AdminObjects surface in mod/objects: the op, a query
// that binds its required arguments, and the nouns the call is expected to
// declare.
type adminOp struct {
	name     string
	op       func(*Module) any
	args     string
	wantID   *astral.ObjectID
	wantRepo astral.String8
}

func adminObjectsOps(id *astral.ObjectID) []adminOp {
	return []adminOp{
		{"objects.delete", func(m *Module) any { return m.OpDelete }, "?repo=local&id=" + id.String(), id, "local"},
		{"objects.purge", func(m *Module) any { return m.OpPurge }, "?repo=local", nil, "local"},
		{"objects.remove_repository", func(m *Module) any { return m.OpRemoveRepository }, "?name=local", nil, "local"},
	}
}

// TestAdminObjectsRefusesCallerWithoutPermits is the coverage measure for the
// AdminObjects action in mod/objects: every destructive op must ask before it
// acts, and must reject when the answer is no.
//
// The module is a bare struct — no repositories, no database, no logger. An op
// that reached past its authorization check would panic on a nil field, so
// "destroys nothing" is enforced by construction as well as by the byte count.
func TestAdminObjectsRefusesCallerWithoutPermits(t *testing.T) {
	id := testObjectID()
	caller := astral.GenerateIdentity()

	for _, op := range adminObjectsOps(id) {
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

			action, ok := actions[0].(*auth.AdminObjectsAction)
			if !ok {
				t.Fatalf("%s named %q; want %q", op.name, actions[0].ObjectType(), (&auth.AdminObjectsAction{}).ObjectType())
			}

			if !action.Actor().IsEqual(caller) {
				t.Fatalf("%s named actor %v; want the caller %v", op.name, action.Actor(), caller)
			}

			switch {
			case (action.ObjectID == nil) != (op.wantID == nil):
				t.Fatalf("%s declared object %v; want %v", op.name, action.ObjectID, op.wantID)
			case action.ObjectID != nil && !action.ObjectID.IsEqual(op.wantID):
				t.Fatalf("%s declared object %v; want %v", op.name, action.ObjectID, op.wantID)
			}

			if action.Repo != op.wantRepo {
				t.Fatalf("%s declared repo %q; want %q", op.name, action.Repo, op.wantRepo)
			}
		})
	}
}
