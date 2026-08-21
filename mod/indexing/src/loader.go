package indexing

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/indexing"
)

type Loader struct{}

// Load constructs and initialises the indexing module: loads config, registers
// op-router handlers, and runs the DB auto-migration before returning.
func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	var err error
	var mod = &Module{
		node:   node,
		config: defaultConfig,
		log:    log,
		assets: assets,
	}

	_ = assets.LoadYAML(indexing.ModuleName, &mod.config)

	err = mod.router.AddStructPrefix(mod, "Op")
	if err != nil {
		return nil, err
	}

	mod.db, err = newDB(assets.Database())
	if err != nil {
		return nil, err
	}

	err = mod.db.autoMigrate()
	if err != nil {
		return nil, err
	}

	return mod, err
}

func init() {
	if err := core.RegisterModule(indexing.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
