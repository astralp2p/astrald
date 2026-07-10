package src

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/bip137sig"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	mod := &Module{
		node:   node,
		log:    log,
		assets: assets,
	}

	mod.router.AddStructPrefix(mod, "Op")

	return mod, nil
}

func init() {
	if err := core.RegisterModule(bip137sig.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
