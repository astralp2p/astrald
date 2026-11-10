package services

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/services"
	"github.com/astralp2p/astrald/mod/services/src/coordinator"
)

const ModuleName = "services"

// writeTimeout bounds one write to a discovery consumer.
// note: provisional value; the design sets it:
// https://wiki.satforge.dev/doc/services-api-implementation-plan-zah9USQT2q
const writeTimeout = 10 * time.Second

type Module struct {
	Deps

	node   astral.Node
	log    *log.Logger
	router routing.OpRouter
	coord  *coordinator.Coordinator
	links  linkWaiters

	visibility visibilityConfig
}

var _ services.Module = &Module{}

// Run applies every change of the private list until ctx ends.
func (mod *Module) Run(ctx *astral.Context) error {
	for list := range mod.visibility.Private.Follow(ctx) {
		mod.coord.SetPrivate(list)
	}
	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return ModuleName
}
