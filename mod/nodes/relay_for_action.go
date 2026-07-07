package nodes

import (
	"io"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
)

// RelayForAction requests permission for Actor to relay traffic for ForID.
type RelayForAction struct {
	auth.Action
	ForID *astral.Identity
}

func (RelayForAction) ObjectType() string { return "mod.nodes.relay_for_action" }

func (a RelayForAction) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *RelayForAction) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(a).ReadFrom(r)
}

func (a RelayForAction) ApplyConstraints(cs *astral.Bundle) bool {
	return cs == nil || len(cs.Objects()) == 0
}

func init() { _ = astral.Add(&RelayForAction{}) }
