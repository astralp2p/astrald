package ip

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/ip"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, l *log.Logger) (core.Module, error) {
	mod := &Module{
		node: node,
		log:  l,
	}

	mod.router.AddStructPrefix(mod, "Op")

	return mod, nil
}

func init() {
	if err := core.RegisterModule(ip.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
