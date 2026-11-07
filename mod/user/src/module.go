package user

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/user"
)

var _ user.Module = &Module{}

type Module struct {
	Deps
	OptionalDeps
	ctx    *astral.Context
	config Config
	file   FileConfig
	node   astral.Node
	log    *log.Logger
	assets assets.Assets
	db     *DB
	router routing.OpRouter

	ready chan struct{}

	sibs sig.Map[string, Sibling]

	// policy persists the admission delegates; an unset value selects
	// accept-all. GetSwarmJoinPolicy, GetSwarmInvitePolicy and
	// GetContractAcceptPolicy read it on every decision.
	policy policyConfig

	// mu makes the claim check and the claim one transition; see claim.
	mu sync.Mutex
	// linkerMu makes the sibling linker's check, schedule and register one step; see runSiblingLinker.
	linkerMu sync.Mutex

	// dials limits the nearby contact attempts per node; see onPeerSeen.
	dials *dialMap
	// readPeerContract reads a nearby peer's contract; see contactPeer.
	readPeerContract peerContractReader
	// now is the clock of the nearby contact policy.
	now func() time.Time
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

// runSiblingLinker starts a link task for every swarm member that has none.
//
// why linkerMu: PeerSeen events, pushed contracts and contract changes run the
// linker at once, and two unserialized runs each start a task for one member.
func (mod *Module) runSiblingLinker() {
	mod.linkerMu.Lock()
	defer mod.linkerMu.Unlock()

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

		mod.addSibling(node, scheduledAction.Cancel, maintainLinkAction.Wake)
	}
}
