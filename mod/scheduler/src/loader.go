package scheduler

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/scheduler"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, l *log.Logger) (core.Module, error) {
	mod := &Module{
		node:  node,
		log:   l,
		ready: make(chan struct{}),
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(scheduler.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
