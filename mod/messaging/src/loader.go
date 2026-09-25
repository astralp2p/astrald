package messaging

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	messagingmod "github.com/astralp2p/astrald/mod/messaging"
)

type Loader struct{}

// Load instantiates the messaging Module: loads config, migrates the database
// schema, mirrors the hosting index into memory and registers Op-prefixed
// struct methods as router operations.
func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	mod := &Module{
		config: defaultConfig,
		node:   node,
		log:    log,
	}

	_ = assets.LoadYAML(messagingmod.ModuleName, &mod.config)

	mod.router.AddStructPrefix(mod, "Op")

	mod.db = &DB{assets.Database()}

	if err := mod.db.Migrate(); err != nil {
		return nil, err
	}

	if err := mod.loadMailboxes(); err != nil {
		return nil, err
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(messagingmod.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
