package user

import (
	"io"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
)

// ExpelAction requests permission for Actor to expel Subject from the user's swarm.
type ExpelAction struct {
	auth.Action
	Subject *astral.Identity
}

func (ExpelAction) ObjectType() string { return "mod.user.expel_action" }

func (a ExpelAction) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *ExpelAction) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(a).ReadFrom(r)
}

func (a ExpelAction) ApplyConstraints(cs *astral.Bundle) bool {
	return cs == nil || len(cs.Objects()) == 0
}

func init() { _ = astral.Add(&ExpelAction{}) }
