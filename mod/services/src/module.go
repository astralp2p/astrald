package services

import (
	"sync"
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

	// authority holds the open follows and bindings that authority changes
	// re-check.
	authority authorityWatch

	// membersMu makes reading the member set and handing it to the
	// coordinator one step, so a stale read never replaces a newer set.
	membersMu sync.Mutex

	visibility visibilityConfig
}

var _ services.Module = &Module{}

// Run seeds the swarm member set, starts re-checking open follows and bindings
// on authority changes, then applies every change of the private list until
// ctx ends. Member changes arrive through ReceiveObject.
func (mod *Module) Run(ctx *astral.Context) error {
	select {
	case <-mod.User.Ready():
	case <-ctx.Done():
		return nil
	}
	mod.refreshMembers()
	mod.watchAuthority()
	go mod.runRechecks(ctx)
	for list := range mod.visibility.Private.Follow(ctx) {
		mod.coord.SetPrivate(list)
	}
	return nil
}

// refreshMembers reads the current swarm member set, hands it to the
// coordinator and returns it.
func (mod *Module) refreshMembers() []*astral.Identity {
	mod.membersMu.Lock()
	defer mod.membersMu.Unlock()
	members := mod.swarmMembers()
	mod.coord.SetMembers(members)
	return members
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return ModuleName
}
