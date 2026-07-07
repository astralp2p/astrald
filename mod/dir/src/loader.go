package dir

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/core/assets"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/dir"
)

type Loader struct{}

// Load initializes the dir module: migrates the alias DB schema, sets a default node alias if absent,
// registers the DNS resolver, and installs the built-in "all" and "localnode" identity filters.
func (Loader) Load(node astral.Node, assets assets.Assets, l *log.Logger) (core.Module, error) {
	var err error
	var mod = &Module{
		node:   node,
		log:    l,
		assets: assets,
		ops:    routing.NewOpRouter(),
	}

	_ = assets.LoadYAML(dir.ModuleName, &mod.config)

	mod.ops.AddStructPrefix(mod, "Op")

	mod.db = assets.Database()

	err = mod.db.AutoMigrate(&dbAlias{})
	if err != nil {
		return nil, err
	}

	err = mod.setDefaultAlias()
	if err != nil {
		mod.log.Errorv(1, "error setting default alias: %v", err)
	}

	mod.resolvers.Add(&DNS{Module: mod})

	// add default filters
	mod.filters.Set("all", astral.AllowAll)
	mod.filters.Set("localnode", func(identity *astral.Identity) bool {
		return identity.IsEqual(mod.node.Identity())
	})

	mod.SetDefaultFilters("all")

	return mod, nil
}

func init() {
	if err := core.RegisterModule(dir.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
