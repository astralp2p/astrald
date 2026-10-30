package tree

import (
	"errors"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/tree"
	"github.com/astralp2p/astrald/resources"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	var err error
	var mod = &Module{
		node:      node,
		config:    defaultConfig,
		log:       log,
		assets:    assets,
		nodeValue: map[int]*sig.Queue[astral.Object]{},
	}

	if err := assets.LoadYAML(tree.ModuleName, &mod.config); err != nil && !errors.Is(err, resources.ErrNotFound) {
		return nil, err
	}

	mod.router.AddStructPrefix(mod, "Op")

	mod.db = &DB{assets.Database()}

	mod.mounts.Set("/", &Node{mod: mod})

	err = mod.db.AutoMigrate(&dbNode{})

	return mod, err
}

func init() {
	if err := core.RegisterModule(tree.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
