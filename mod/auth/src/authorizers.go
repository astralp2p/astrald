package auth

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/auth"
)

func (mod *Module) AuthorizeSudo(ctx *astral.Context, a *auth.SudoAction) bool {
	return a.Actor().IsEqual(a.AsID)
}
