package auth

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/auth"
)

func (mod *Module) AuthorizeSudo(ctx *astral.Context, a *auth.SudoAction) bool {
	return a.Actor().IsEqual(a.AsID)
}
