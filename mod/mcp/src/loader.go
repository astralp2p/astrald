package mcp

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	mcpmod "github.com/astralp2p/astrald/mod/mcp"
)

type Loader struct{}

// Load instantiates the mcp Module: loads config, migrates the database
// schema, and registers Op-prefixed struct methods as router operations.
func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	var err error

	mod := &Module{
		config: defaultConfig,
		node:   node,
		log:    log,
	}

	_ = assets.LoadYAML(mcpmod.ModuleName, &mod.config)

	// why a start fails on a tool it cannot read: a tool the agent never sees
	// is a deployment that believes it exposed one, and the failure is silent
	// at every later moment.
	mod.tools, err = readDeclaredTools(mod.config.Tools)
	if err != nil {
		return nil, err
	}

	mod.router.AddStructPrefix(mod, "Op")

	mod.db = &DB{assets.Database()}

	err = mod.db.MigrateAgents()
	if err != nil {
		return nil, err
	}

	err = mod.db.MigrateMessages()
	if err != nil {
		return nil, err
	}

	rows, err := mod.db.ListAgents()
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		_ = mod.agentIDs.Add(r.Identity.String())
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(mcpmod.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
