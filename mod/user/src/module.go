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
	// linkRetryAfter replaces time.After as the clock of every MaintainLinkTask backoff while set.
	// note: a test seam, set through SetLinkRetryClock.
	linkRetryAfter atomic.Pointer[func(time.Duration) <-chan time.Time]

	// askDigest asks a sibling for its relay digest; nil selects askRelayDigest.
	askDigest askDigestFunc
	// digestTimeout bounds one digest query; zero selects relayDigestTimeout.
	digestTimeout time.Duration

	// joinTarget is the node a running user.join is joining; nil while no join
	// runs. See recognizeJoinTarget.
	joinTarget atomic.Pointer[astral.Identity]

	// store is this node's copy of SwarmState, nil until Run opens it.
	store atomic.Pointer[swarmStore]
	// swarmLinkHook replaces onSwarmLink in exchangeSwarmWithLinked.
	// note: a test seam; nil in the running node.
	swarmLinkHook func(peer *astral.Identity)
	// swarmDial replaces the query to a peer's exchange ops; nil selects routerSwarmPeer.
	// note: a test seam; nil in the running node.
	swarmDial func(target *astral.Identity) swarmPeer

	// swarmView reads SwarmState; entryCheck decides what enters it.
	swarmView  *swarmView
	entryCheck *swarmEntryCheck
	// carrier carries the JoinRequests that name this node (user.carry_join).
	carrier *swarmCarrier
	// joiner keeps this node's own JoinRequest while it joins (user.join).
	joiner *swarmJoiner
	// indexedEntries holds the SwarmState contracts already handed to the auth
	// index, and the SwarmState expels already stored as bans.
	indexedEntries sig.Map[astral.ObjectID, struct{}]

	// banned holds the subjects the active user banned; see isBanned.
	banned atomic.Pointer[map[string]struct{}]
	// banAnswers holds the banned subjects that answered their own ban pushed
	// from this node; see AdmitLink.
	banAnswers sig.Set[string]
}

func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx.IncludeZone(astral.ZoneNetwork)
	<-mod.Scheduler.Ready()
	// why: before the active contract applies, so the node's own membership is not added to a closed store.
	if err := mod.openSwarmState(); err != nil {
		mod.log.Error("open swarm state: %v", err)
	} else {
		go mod.runSwarmSync(mod.ctx)
	}

	activeContractFollow := mod.config.ActiveContract.Follow(ctx)
	mod.onActiveContractChanged(<-activeContractFollow)
	mod.recordSwarm()
	close(mod.ready)
	go mod.runReconcile(mod.ctx)
	go mod.runPeriodicSiblingSync(mod.ctx)
	go mod.runSwarmWatcher(mod.ctx)
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

// runSiblingLinker starts a link task for every swarm member that has none,
// runs onNewMember for it, and stops the link task of every sibling that is
// no longer a swarm member.
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

		mod.onNewMember(node)
	}
}

// onNewMember reacts to a member the sibling linker lists for the first time:
// mod/nodes learns its endpoints, and a linked member gets a sibling sync.
//
// why: a membership reaches this node only through SwarmState sync, which
// brings no endpoints, so a new member's link task would have none to dial;
// the pull's EndpointsUpdatedEvent wakes the task.
// why the sync: the LinkCreatedEvent sync skips a node linked before it was a
// member, as a joiner is with its carrier, and before G7 the pushed node
// contract started that sync (receiveSignedContract); it carries the relay
// contracts of the swarm's apps.
func (mod *Module) onNewMember(node *astral.Identity) {
	go mod.Nodes.LearnMemberEndpoints(node)

	if mod.Nodes.IsLinked(node) {
		if _, err := mod.Scheduler.Schedule(mod.NewSyncNodesTask(node)); err != nil {
			mod.log.Error("scheduling a sync with new member %v: %v", node, err)
		}
	}
}
