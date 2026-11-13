package vfs

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
)

func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	if err = core.Inject(mod.node, &mod.Deps); err != nil {
		return
	}

	// note: the module runs without the user module; the node is then the only root caller.
	core.Inject(mod.node, &mod.OptionalDeps)

	return
}
