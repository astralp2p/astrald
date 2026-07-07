package auth

import (
	"github.com/cryptopunkscc/astral-go/api/auth"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

// SudoAction requests permission to act as AsID.
// ActorID (from base) is the requesting identity; AsID is the target identity.
type SudoAction struct {
	auth.Action
	AsID *astral.Identity
}

func (SudoAction) ObjectType() string { return "mod.auth.sudo_action" }

func (a SudoAction) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *SudoAction) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(a).ReadFrom(r)
}

func init() { _ = astral.Add(&SudoAction{}) }
