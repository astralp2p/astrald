package user

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
)

var _ user.Module = &Module{}
var _ user.SwarmState = &Module{}
var _ user.SwarmView = &Module{}

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

	// swarm is the member set publishSwarm last emitted events for.
	swarm memberSet

	// mu makes the claim check and the claim one transition; see claim.
	mu sync.Mutex

	// syncGate runs one sync per sibling at a time after a partial link close.
	syncGate syncGate

	// dials limits the nearby contact attempts per node; see onPeerSeen.
	dials *dialMap
	// readPeerContract reads a nearby peer's contract; see contactPeer.
	readPeerContract peerContractReader
	// now is the clock of the nearby contact policy and of the member expiry timer.
	now func() time.Time

	// askDigest asks a sibling for its relay digest; nil selects askRelayDigest.
	askDigest askDigestFunc
	// digestTimeout bounds one digest query; zero selects relayDigestTimeout.
	digestTimeout time.Duration

	// joinTarget is the node a running user.join is joining; nil while no join
	// runs. See recognizeJoinTarget.
	joinTarget atomic.Pointer[astral.Identity]

	// swarmState is this node's copy of SwarmState; LoadDependencies builds it and Run opens it.
	swarmState *swarmStore
	// entryRules is the entry check of swarmState; LoadDependencies gives it the crypto module.
	entryRules *swarmset.Rules
	// publisher emits SwarmState's events; swarmFeed reports its changes.
	publisher *swarmPublisher
	swarmFeed SwarmFeed

	// swarmView reads SwarmState and answers usermod.SwarmView for the module; Load builds it.
	*swarmView
	// joinConfirmations holds the PrivilegedMemberships the user signed with user.confirm_join on this node, by the
	// entry ID of their JoinRequest, until the carrier hands one to its joiner. It lives in memory: after a restart
	// the user confirms again, and the body is the same.
	joinConfirmations sig.Map[string, *auth.SignedContract]
	// carrier carries the JoinRequests that name this node (user.carry_join).
	carrier *swarmCarrier
	// joiner keeps this node's own JoinRequest while it joins (user.join).
	joiner *swarmJoiner

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
	// why: nothing is emitted for what is already stored; consumers read the view once in their Run. The auth index is the exception: no event indexes a contract counted before record.
	mod.publisher.record()
	mod.indexRecordedContracts()
	close(mod.ready)
	go mod.runReconcile(mod.ctx)
	go mod.runPeriodicSiblingSync(mod.ctx)
	go mod.runSwarmFeed(mod.ctx)
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
