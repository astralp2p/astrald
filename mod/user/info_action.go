package user

import (
	"io"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
)

// InfoAction requests permission for Actor to read the active contract's metadata (user.info).
type InfoAction struct {
	auth.Action
}

func (InfoAction) ObjectType() string { return "mod.user.info_action" }

func (a InfoAction) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *InfoAction) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(a).ReadFrom(r)
}

func (a InfoAction) ApplyConstraints(cs *astral.Bundle) bool {
	return cs == nil || len(cs.Objects()) == 0
}

func init() { _ = astral.Add(&InfoAction{}) }
