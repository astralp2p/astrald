package shell

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/core/assets"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/shell"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	var err error
	var mod = &Module{
		node:   node,
		config: defaultConfig,
		log:    log,
		assets: assets,
	}

	_ = assets.LoadYAML(shell.ModuleName, &mod.config)

	shellOps := routing.NewOpRouter()
	err = shellOps.AddStructPrefix(mod, "Op")
	if err != nil {
		return nil, err
	}

	spec, err := shellOps.GetOp("spec")
	if err != nil {
		return nil, err
	}
	root := routing.NewOpRouter()
	root.AddOp(".spec", spec)

	mod.scopes = routing.NewScopeRouter(root)
	mod.scopes.Add(shell.ModuleName, shellOps)

	return mod, err
}

func init() {
	if err := core.RegisterModule(shell.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
