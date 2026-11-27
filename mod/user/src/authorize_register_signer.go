package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeRegisterSigner allows the user (userOrSelf) to claim any key on a
// crypto.register_signer binding, and answers no for every other identity.
//
// why: each built-in action's handler names the user (operator decision,
// 2026-11-27).
// note: a claim grants no signature; this node verifies every signature a
// holder returns (auth.RegisterSignerAction).
func (mod *Module) AuthorizeRegisterSigner(ctx *astral.Context, a *auth.RegisterSignerAction) bool {
	return !a.Actor().IsZero() && a.Actor().IsEqual(mod.userOrSelf())
}
