package services

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/services"
	"github.com/astralp2p/astrald/mod/services/src/coordinator"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	var mod = &Module{
		node: node,
		log:  log,
		// why: the coordinator exists from Load, so native modules can register
		// from their Prepare, which runs concurrently across modules.
		coord: coordinator.New(coordinator.DefaultConfig()),
	}

	mod.router.AddStructPrefix(mod, "Op")

	return mod, nil
}

func init() {
	if err := core.RegisterModule(services.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
