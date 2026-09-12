package tree

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// seeNodeStateOp is one row of the SeeNodeState surface in mod/tree: the op and
// a query that binds its required arguments.
type seeNodeStateOp struct {
	name string
	op   func(*Module) any
	args string
}

func seeNodeStateOps() []seeNodeStateOp {
	return []seeNodeStateOp{
		{"tree.get", func(m *Module) any { return m.OpGet }, "?path=/"},
		{"tree.list", func(m *Module) any { return m.OpList }, "?path=/"},
	}
}

// TestSeeNodeStateRefusesCallerWithoutPermits is the coverage measure for the
// SeeNodeState action in mod/tree: every guarded read must ask before it walks
// a path, and must reject when the answer is no.
//
// note: the module is a bare struct with no mounts, so an op that walked a path
// past its authorization check would panic on the nil root.
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

// seeNodeStateLeaf is a tree.Node holding one value and no subnodes.
//
// note: the embedded nil interface panics on any other method, so the allowed
// path reaches only Get and Sub.
type seeNodeStateLeaf struct {
	tree.Node
	value astral.Object
}

func (n *seeNodeStateLeaf) Get(*astral.Context, bool) (<-chan astral.Object, error) {
	out := make(chan astral.Object, 1)
	out <- n.value
	close(out)
	return out, nil
}

func (n *seeNodeStateLeaf) Sub(*astral.Context) (map[string]tree.Node, error) {
	return map[string]tree.Node{}, nil
}

// TestSeeNodeStateAnswersHolder is the allowed path: a caller holding the action
// receives each op's answer from the mounted root.
func TestSeeNodeStateAnswersHolder(t *testing.T) {
	for _, op := range seeNodeStateOps() {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: true}
			mod := &Module{Deps: Deps{Auth: authority}}
			mod.mounts.Set("/", &seeNodeStateLeaf{value: astral.NewString8("v")})
			w := newRecordingWriter()

			if err := route(t, op.op(mod), astral.GenerateIdentity(), op.name+op.args, w); err != nil {
				t.Fatalf("%s refused a caller holding the action: %v", op.name, err)
			}

			select {
			case <-w.closed:
			case <-time.After(5 * time.Second):
				t.Fatalf("%s did not finish for a caller holding the action", op.name)
			}

			if w.written() == 0 {
				t.Fatalf("%s wrote nothing to a caller holding the action", op.name)
			}

			if n := len(authority.recorded()); n != 1 {
				t.Fatalf("%s made %d authorization calls; want exactly 1", op.name, n)
			}
		})
	}
}
