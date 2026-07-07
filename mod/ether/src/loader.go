package ether

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/core/assets"
	"github.com/cryptopunkscc/astrald/mod/ether"
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

	_ = assets.LoadYAML(ether.ModuleName, &mod.config)

	// LAN discovery is optional. If UDP binding fails, module loads with nil socket; broadcasts become no-ops, API remains functional.
	if err = mod.setupSocket(); err != nil {
		log.Error("LAN discovery disabled: %v", err)
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(ether.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
