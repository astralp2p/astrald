package user

import (
	"sync"
	"sync/atomic"
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
	Transports
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

	// swarm is the member set publishSwarm last emitted events for.
	swarm memberSet

	// policy persists the admission delegates; an unset value selects the
	// policies.* config value, whose code default is reject_all.
	// GetSwarmJoinPolicy, GetSwarmInvitePolicy and GetContractAcceptPolicy read
	// it on every decision.
	policy policyConfig

	// mu makes the claim check and the claim one transition; see claim.
	mu sync.Mutex
	// linkerMu makes the sibling linker's check, schedule and register one step; see runSiblingLinker.
	linkerMu sync.Mutex

	// syncGate runs one sync per sibling at a time after a partial link close.
	syncGate syncGate

	// dials limits the nearby contact attempts per node; see onPeerSeen.
	dials *dialMap
	// readPeerContract reads a nearby peer's contract; see contactPeer.
	readPeerContract peerContractReader
	// now is the clock of the nearby contact policy and of the member expiry timer.
	now func() time.Time
	// after is the clock of the ban push backoff; see pushUntilDelivered.
	after func(time.Duration) <-chan time.Time

	// askDigest asks a sibling for its relay digest; nil selects askRelayDigest.
	askDigest askDigestFunc
	// digestTimeout bounds one digest query; zero selects relayDigestTimeout.
	digestTimeout time.Duration

	// joinTarget is the node a running user.join is joining; nil while no join
	// runs. See recognizeJoinTarget.
	joinTarget atomic.Pointer[astral.Identity]

	// banned holds the subjects the active user banned; see isBanned.
	banned atomic.Pointer[map[string]struct{}]
	// banAnswers holds the banned subjects that answered their own ban pushed
	// from this node; see AdmitLink.
	banAnswers sig.Set[string]
}

func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx.IncludeZone(astral.ZoneNetwork)
	<-mod.Scheduler.Ready()

	activeContractFollow := mod.config.ActiveContract.Follow(ctx)
	mod.onActiveContractChanged(<-activeContractFollow)
	mod.recordSwarm()
	close(mod.ready)
	go mod.runReconcile(mod.ctx)
	go mod.runPeriodicSiblingSync(mod.ctx)
	go func() {
		for contract := range activeContractFollow {
			mod.onActiveContractChanged(contract)
		}
	}()

	<-ctx.Done()

	mod.swarm.mu.Lock()
	defer mod.swarm.mu.Unlock()
	mod.stopExpiry()

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

// runSiblingLinker starts a link task for every swarm member that has none and
// stops the link task of every sibling that is no longer a swarm member.
//
// why linkerMu: PeerSeen events, pushed contracts and contract changes run the
// linker at once, and two unserialized runs each start a task for one member.
// why the early return: a failed contract query reads as an empty swarm, and
// pruning on it would stop every sibling's link task.
func (mod *Module) runSiblingLinker() {
	mod.linkerMu.Lock()
	defer mod.linkerMu.Unlock()

	swarm, err := mod.localSwarm()
	if err != nil {
		return
	}
	mod.pruneSiblings(swarm)

	for _, node := range swarm {
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
