package user

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/core/assets"
	"github.com/cryptopunkscc/astrald/mod/user"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	var err error
	var mod = &Module{
		node:   node,
		log:    log,
		assets: assets,
		ready:  make(chan struct{}),
	}

	_ = assets.LoadYAML(user.ModuleName, &mod.config)

	mod.ctx = astral.NewContext(nil).WithIdentity(node.Identity())

	mod.router.AddStructPrefix(mod, "Op")

	mod.db = &DB{DB: assets.Database(), mod: mod}

	err = mod.db.AutoMigrate(&dbAsset{}, &dbExpulsion{})
	if err != nil {
		return nil, err
	}

	return mod, err
}

func init() {
	if err := core.RegisterModule(user.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
