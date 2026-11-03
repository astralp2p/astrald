package objects

import (
	"errors"
	"strings"
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
		{"objects.probe", func(m *Module) any { return m.OpProbe }, "?id=" + id.String() + "&repo=main", id, "main"},
		{"objects.describe", func(m *Module) any { return m.OpDescribe }, "?id=" + id.String(), id, ""},
		{"objects.scan", func(m *Module) any { return m.OpScan }, "?repo=main", nil, "main"},
		{"objects.search", func(m *Module) any { return m.OpSearch }, "?q=anything&repo=main", nil, "main"},
		{"objects.find", func(m *Module) any { return m.OpFind }, "?id=" + id.String(), id, ""},
		{"objects.repositories", func(m *Module) any { return m.OpRepositories }, "", nil, ""},
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

			// note: objects.read streams raw bytes, so it keeps the reject; an error_message
			// would reach its caller as payload.
			if strings.HasPrefix(op.name, "objects.read") {
				var rejected *astral.ErrRejected
				if !errors.As(err, &rejected) || w.written() != 0 {
					t.Fatalf("got err %v and %d bytes; want a rejection and no bytes", err, w.written())
				}
			} else {
				if err != nil {
					t.Fatalf("route: %v", err)
				}
				if got, want := w.refusal(t), auth.ErrNotPermitted.Error(); got != want {
					t.Fatalf("answered %q; want %q", got, want)
				}
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
// objects.repositories is the subject because a bare module holds no
// repository, so it answers in full with the terminator alone.
func TestSeeObjectsAllowsGrantedCaller(t *testing.T) {
	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority}}
	w := newRecordingWriter()

	if err := route(t, mod.OpRepositories, astral.GenerateIdentity(), "objects.repositories", w); err != nil {
		t.Fatalf("granted caller was refused: %v", err)
	}

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("objects.repositories did not finish answering a granted caller")
	}

	if w.written() == 0 {
		t.Fatal("objects.repositories wrote nothing to a granted caller")
	}
}
