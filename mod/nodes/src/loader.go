package nodes

import (
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/core/assets"
	"github.com/cryptopunkscc/astrald/mod/nodes"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	var err error
	var mod = &Module{
		node:   node,
		log:    log,
		assets: assets,
	}

	_ = assets.LoadYAML(nodes.ModuleName, &mod.config)

	mod.linkPool = NewLinkPool(mod)

	mod.RegisterLinkStrategy(nodes.StrategyBasic, &BasicLinkStrategyFactory{mod: mod, networks: []string{"tcp"}})
	mod.RegisterLinkStrategy(nodes.StrategyTor, &TorLinkStrategyFactory{
		mod:     mod,
		network: nodes.StrategyTor,
		config: TorLinkStrategyConfig{
			QuickRetries:      2,
			Retries:           3,
			SignalTimeout:     60 * time.Second,
			BackgroundTimeout: 360 * time.Second,
		},
	})
	mod.RegisterLinkStrategy(nodes.StrategyNAT, &NATLinkStrategyFactory{mod: mod})

	mod.db = &DB{assets.Database()}
	mod.dbResolver = &DBEndpointResolver{mod: mod}
	mod.resolvers.Add(mod.dbResolver)

	err = mod.db.AutoMigrate(&dbEndpoint{})
	if err != nil {
		return nil, err
	}

	mod.router.AddStructPrefix(mod, "Op")

	return mod, err
}

func init() {
	if err := core.RegisterModule(nodes.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
