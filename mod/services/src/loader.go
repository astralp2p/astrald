package services

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/core/assets"
	"github.com/cryptopunkscc/astrald/mod/services"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	var mod = &Module{
		node: node,
		log:  log,
	}

	mod.db = &DB{db: assets.Database()}

	if err := mod.db.Migrate(); err != nil {
		return nil, err
	}

	mod.router.AddStructPrefix(mod, "Op")

	return mod, nil
}

func init() {
	if err := core.RegisterModule(services.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
