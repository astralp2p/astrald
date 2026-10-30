package gateway

import (
	"errors"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/gateway"
	"github.com/astralp2p/astrald/resources"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	mod := &Module{
		node:            node,
		log:             log,
		config:          defaultConfig,
		configEndpoints: make(map[string]exonet.Endpoint),
	}

	if err := assets.LoadYAML(gateway.ModuleName, &mod.config); err != nil && !errors.Is(err, resources.ErrNotFound) {
		return nil, err
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(gateway.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
