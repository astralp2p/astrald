package user

import (
	"io"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
)

// AdoptAction requests permission for Actor to adopt Subject into the user's swarm.
type AdoptAction struct {
	auth.Action
	Subject *astral.Identity
}

func (AdoptAction) ObjectType() string { return "mod.user.adopt_action" }

func (a AdoptAction) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *AdoptAction) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(a).ReadFrom(r)
}

func (a AdoptAction) ApplyConstraints(cs *astral.Bundle) bool {
	return cs == nil || len(cs.Objects()) == 0
}

func init() { _ = astral.Add(&AdoptAction{}) }
