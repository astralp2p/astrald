package objects

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// testObjectID is a well-formed id; no op in the refusal table gets far enough to
// look it up.
func testObjectID() *astral.ObjectID {
	id := &astral.ObjectID{Size: 40}
	for i := range id.Hash {
		id.Hash[i] = byte(i)
	}
	return id
}

// seeOp is one row of the SeeObjects surface: the op, a query that binds its
// required arguments, and the nouns the call is expected to declare.
type seeOp struct {
	name     string
	op       func(*Module) any
	args     string
	wantID   *astral.ObjectID
	wantRepo astral.String8
}

func seeObjectsOps(id *astral.ObjectID) []seeOp {
	return []seeOp{
		{"objects.read", func(m *Module) any { return m.OpRead }, "?id=" + id.String() + "&repo=main", id, "main"},
		{"objects.load", func(m *Module) any { return m.OpLoad }, "?id=" + id.String() + "&repo=main", id, "main"},
		{"objects.contains", func(m *Module) any { return m.OpContains }, "?repo=main&id=" + id.String(), id, "main"},
		{"objects.get_type", func(m *Module) any { return m.OpGetType }, "?id=" + id.String(), id, ""},
		{"objects.probe", func(m *Module) any { return m.OpProbe }, "?id=" + id.String() + "&repo=main", id, "main"},
		{"objects.describe", func(m *Module) any { return m.OpDescribe }, "?id=" + id.String(), id, ""},
		{"objects.scan", func(m *Module) any { return m.OpScan }, "?repo=main", nil, "main"},
		{"objects.search", func(m *Module) any { return m.OpSearch }, "?q=anything&repo=main", nil, "main"},
		{"objects.find", func(m *Module) any { return m.OpFind }, "?id=" + id.String(), id, ""},
		{"objects.repositories", func(m *Module) any { return m.OpRepositories }, "", nil, ""},
		{"objects.blueprints", func(m *Module) any { return m.OpBlueprints }, "", nil, ""},
		{"objects.get_blueprint", func(m *Module) any { return m.OpGetBlueprint }, "?type=" + (&astral.Blob{}).ObjectType(), nil, ""},
	}
}

// TestSeeObjectsRefusesCallerWithoutPermits is the coverage measure for the
// SeeObjects action: every read op in the module must ask before it acts, and
// must reject when the answer is no.
//
// The module is a bare struct — no repositories, no database, no logger. An op
// that reached past its authorization check would panic on a nil field, so
// "performs no read" is enforced by construction as well as by the byte count.
func TestSeeObjectsRefusesCallerWithoutPermits(t *testing.T) {
	id := testObjectID()
	caller := astral.GenerateIdentity()

	for _, op := range seeObjectsOps(id) {
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

			action, ok := actions[0].(*auth.SeeObjectsAction)
			if !ok {
				t.Fatalf("%s named %q; want %q", op.name, actions[0].ObjectType(), (&auth.SeeObjectsAction{}).ObjectType())
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

// TestSeeObjectsAllowsGrantedCaller covers the granted path once: the same
// refusal machinery must let a permitted caller through. Without it the refusal
// table above would still pass on an op that rejects unconditionally.
//
// objects.blueprints is the subject because it reads no repository, so a bare
// module answers it in full.
func TestSeeObjectsAllowsGrantedCaller(t *testing.T) {
	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority}}
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
