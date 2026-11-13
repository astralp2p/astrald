package vfs

import (
	"errors"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	vfsmod "github.com/astralp2p/astrald/mod/vfs"
	"github.com/astralp2p/astrald/resources"
)

type Loader struct{}

// Load reads the config, migrates the tables, registers the Op-prefixed methods of the module as operations, and
// makes every VFS whose root ObjectID is cleared pending for the background compute.
func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	mod := &Module{
		config:   defaultConfig,
		node:     node,
		identity: node.Identity(),
		log:      log,
	}

	if err := assets.LoadYAML(vfsmod.ModuleName, &mod.config); err != nil && !errors.Is(err, resources.ErrNotFound) {
		return nil, err
	}

	warnRemovedKeys(assets, log)
	mod.config.checkDirectoryValues(log)

	if err := mod.router.AddStructPrefix(mod, "Op"); err != nil {
		return nil, err
	}

	mod.db = &DB{assets.Database()}

	if err := mod.db.migrate(); err != nil {
		return nil, err
	}

	mod.setupDirectories(systemClock{})

	if err := mod.seedPending(); err != nil {
		return nil, err
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(vfsmod.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
