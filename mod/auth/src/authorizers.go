package auth

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/auth"
)

// AuthorizeSudo allows an identity to act as itself.
//
// why the zero target is refused before the self test: Identity.IsEqual answers
// true when both sides are zero, and Dir.ResolveIdentity maps the empty name and
// "anyone" to the zero identity. An actor that arrives zero would otherwise pass
// the self test against that name. The anonymous identity is not a party, so
// nothing acts as it.
func (mod *Module) AuthorizeSudo(ctx *astral.Context, a *auth.SudoAction) bool {
	return !a.AsID.IsZero() && a.Actor().IsEqual(a.AsID)
}
