package nearby

import (
	"errors"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/resources"
)

const ModuleName = "nearby"

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	mod := &Module{
		node: node,
		log:  log,
	}

	if err := assets.LoadYAML(ModuleName, &mod.config); err != nil && !errors.Is(err, resources.ErrNotFound) {
		return nil, err
	}

	err := mod.router.AddStructPrefix(mod, "Op")
	if err != nil {
		return nil, err
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
