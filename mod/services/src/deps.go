package services

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

type Deps struct {
	Auth authmod.Module
}

func (mod *Module) LoadDependencies(*astral.Context) error {
	return core.Inject(mod.node, &mod.Deps)
}
