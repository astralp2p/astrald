package apphost

import (
	"net"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/core/assets"
	"github.com/cryptopunkscc/astrald/mod/apphost"
)

type Loader struct{}

// Load instantiates the apphost Module: loads config, auto-migrates the database schema,
// and registers Op-prefixed struct methods as router operations.
func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	var err error

	mod := &Module{
		config:    defaultConfig,
		node:      node,
		listeners: make([]net.Listener, 0),
		log:       log,
	}

	_ = assets.LoadYAML(apphost.ModuleName, &mod.config)

	mod.router.AddStructPrefix(mod, "Op")

	// set up the database
	mod.db = &DB{assets.Database()}

	err = mod.db.AutoMigrate(&dbAccessToken{}, &dbLocalApp{}, &dbObjectHold{})
	if err != nil {
		return nil, err
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(apphost.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
