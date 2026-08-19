package mcp

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/mcp"
)

type Loader struct{}

// Load instantiates the mcp Module: loads config, auto-migrates the database
// schema, and registers Op-prefixed struct methods as router operations.
func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	var err error

	mod := &Module{
		config: defaultConfig,
		node:   node,
		log:    log,
	}

	_ = assets.LoadYAML(mcp.ModuleName, &mod.config)

	mod.router.AddStructPrefix(mod, "Op")

	mod.db = &DB{assets.Database()}

	err = mod.db.AutoMigrate(&dbAgent{})
	if err != nil {
		return nil, err
	}

	rows, err := mod.db.ListAgents()
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		_ = mod.agentIDs.Add(r.Identity.String())
		if r.Exposed {
			_ = mod.exposed.Add(r.Identity.String())
		}
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(mcp.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
