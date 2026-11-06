package sys

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/sys"
)

type Loader struct{}

func (Loader) Load(node astral.Node, _ assets.Assets, log *log.Logger) (core.Module, error) {
	mod := &Module{
		node: node,
		log:  log,
	}

	if err := mod.router.AddStructPrefix(mod, "Op"); err != nil {
		return nil, err
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(sys.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
