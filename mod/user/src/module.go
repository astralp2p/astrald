package user

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astral-go/lib/routing"
	"github.com/cryptopunkscc/astrald/core/assets"
	"github.com/cryptopunkscc/astrald/mod/user"
)

var _ user.Module = &Module{}

type Module struct {
	Deps
	ctx    *astral.Context
	config Config
	node   astral.Node
	log    *log.Logger
	assets assets.Assets
	db     *DB
	router routing.OpRouter

	ready chan struct{}

	sibs sig.Map[string, Sibling]
}

func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx.IncludeZone(astral.ZoneNetwork)
	<-mod.Scheduler.Ready()

	activeContractFollow := mod.config.ActiveContract.Follow(ctx)
	mod.onActiveContractChanged(<-activeContractFollow)
	close(mod.ready)
	go func() {
		for contract := range activeContractFollow {
			mod.onActiveContractChanged(contract)
		}
	}()

	<-ctx.Done()

	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

// Ready returns a channel that is closed once Run has applied the initial active contract and is fully initialized.
func (mod *Module) Ready() <-chan struct{} {
	return mod.ready
}

func (mod *Module) String() string {
	return user.ModuleName
}

func (mod *Module) runSiblingLinker() {
	for _, node := range mod.LocalSwarm() {
		if node.IsEqual(mod.node.Identity()) {
			continue
		}

		_, ok := mod.sibs.Get(node.String())
		if ok {
			continue
		}

		maintainLinkAction := mod.NewMaintainLinkTask(node)
		scheduledAction, err := mod.Scheduler.Schedule(maintainLinkAction)
		if err != nil {
			mod.log.Error("error scheduling maintain link action: %v for node %v", err, node)
			continue
		}

		mod.addSibling(node, scheduledAction.Cancel)
	}
}
