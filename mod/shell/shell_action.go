package shell

import (
	"io"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// ShellAction requests an interactive op shell on this node. ActorID (from the
// embedded Action) is the requesting identity.
//
// why: shell.shell mounts every loaded module's op router behind one session,
// so admission is node-wide administration rather than one capability. The
// action exists to carry that admission on the permit rail, where a grant
// records it and a revoke withdraws it.
type ShellAction struct {
	auth.Action
}

func (ShellAction) ObjectType() string { return "mod.shell.shell_action" }

func (a ShellAction) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *ShellAction) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(a).ReadFrom(r)
}

// why: MustAdd, not a discarded Add. A registration that never succeeds and
// says nothing is the defect class that stopped asset sync; this name is new,
// so a panic here can only mean a real collision.
func init() { astral.MustAdd(&ShellAction{}) }
