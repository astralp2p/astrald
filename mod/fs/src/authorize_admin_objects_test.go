package fs

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// adminOp is one row of the AdminObjects surface in mod/fs: the op, a query that
// binds its required arguments, and the nouns the call is expected to declare.
type adminOp struct {
	name     string
	op       func(*Module) any
	args     string
	wantRepo astral.String8
	wantPath astral.String8
}

func adminObjectsOps(path string) []adminOp {
	return []adminOp{
		{
			"fs.new_repo",
			func(m *Module) any { return m.OpNewRepo },
			"?path=" + path + "&name=photos",
			"photos",
			astral.String8(path),
		},
		{
			"fs.new_watch",
			func(m *Module) any { return m.OpNewWatch },
			"?path=" + path + "&name=photos",
			"photos",
			astral.String8(path),
		},
	}
}

// TestAdminObjectsRefusesCallerWithoutPermits is the coverage measure for the
// AdminObjects action in mod/fs: both ops must ask before they act, and must
// reject when the answer is no.
//
// The module is a bare struct — no objects module, no indexer, no logger. An op
// that reached past its authorization check would panic on a nil field, so
// "attaches nothing" is enforced by construction as well as by the byte count.
func TestAdminObjectsRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()
	path := t.TempDir()

	for _, op := range adminObjectsOps(path) {
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

			if action.Repo != op.wantRepo {
				t.Fatalf("%s declared repo %q; want %q", op.name, action.Repo, op.wantRepo)
			}

			if action.Path != op.wantPath {
				t.Fatalf("%s declared path %q; want %q", op.name, action.Path, op.wantPath)
			}
		})
	}
}

// TestAdminObjectsAllowsGrantedCaller covers the granted path: the same refusal
// machinery must let a permitted caller through. Without it the refusal table
// above would still pass on an op that rejects unconditionally.
//
// The path is one that does not exist, so a granted caller gets as far as
// validPath and is answered over the channel instead of reaching the objects
// module a bare test module does not have. Reaching the answer at all is the
// assertion: the query was accepted, not rejected.
func TestAdminObjectsAllowsGrantedCaller(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")

	for _, op := range adminObjectsOps(missing) {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: true}
			mod := &Module{Deps: Deps{Auth: authority}}
			w := newRecordingWriter()

			if err := route(t, op.op(mod), astral.GenerateIdentity(), op.name+op.args, w); err != nil {
				t.Fatalf("%s refused a granted caller: %v", op.name, err)
			}

			w.waitClosed(t)

			if n := w.written(); n == 0 {
				t.Fatalf("%s answered a granted caller with nothing; want the path error", op.name)
			}

			if len(authority.recorded()) != 1 {
				t.Fatalf("%s made %d authorization calls; want exactly 1", op.name, len(authority.recorded()))
			}
		})
	}
}
