package apphost

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
)

func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	if err = core.Inject(mod.node, &mod.Deps); err != nil {
		return
	}

	// optional — apphost can run without user module
	core.Inject(mod.node, &mod.OptionalDeps)

	return
}
