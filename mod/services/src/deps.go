package services

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/user"
)

type Deps struct {
	Auth    authmod.Module
	Objects objects.Module
	User    user.Module
}

func (mod *Module) LoadDependencies(*astral.Context) error {
	return core.Inject(mod.node, &mod.Deps)
}
