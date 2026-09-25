package mcp

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/apphost"
	"github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/messaging"
)

type Deps struct {
	Apphost   apphost.Module
	Auth      auth.Module
	Dir       dir.Module
	Messaging messaging.Module
}

func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	return core.Inject(mod.node, &mod.Deps)
}
