package nodes

import (
	"context"
	"errors"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"sync"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nodes"
	modsecp256k1 "github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/resources"
)

const DefaultWorkerCount = 8
const infoPrefix = "node1"
const featureMux2 = "mux2"

const pingJitter = 1 * time.Second

var _ nodesmod.Module = &Module{}

type Module struct {
	Deps
	config Config
	node   astral.Node
	log    *log.Logger
	assets resources.Resources
	db     *DB
	// ctx is the module context: network zone, node identity, cancelled when Run ends.
	// why: set once by initContext in Load, before any transport or router calls in, and
	// never reassigned, so a strategy that joins before Run reads a live, race-free context.
	ctx    *astral.Context
	cancel context.CancelFunc
	router routing.OpRouter

	dbResolver *DBEndpointResolver
	resolvers  sig.Set[nodesmod.EndpointResolver]

	// admitters are the link admission rules; see link_admission.go.
	admitters sig.Set[nodesmod.LinkAdmitter]

	observedEndpoints sig.Map[string, ObservedEndpoint] // key is IP string

	linkPool *LinkPool

	// inboundNegotiations counts the inbound link negotiations past the handshake, per peer.
	inboundNegotiations inFlight
	// dials counts the strategy dials in progress, per target.
	dials inFlight

	strategyFactories sig.Map[string, nodesmod.StrategyFactory]
	upgraders         sig.Map[string, *sig.Switch]

	// mu guards privateKey, which the first link in either direction loads.
	mu         sync.Mutex
	privateKey *crypto.PrivateKey

	pull relayPull

	// retrieve replaces the link pool's retrieval of a link to a query's target
	// or relay; nil is linkPool.RetrieveLink with the Basic and Tor strategies.
	// why: a field, so tests block or count a retrieval without a link strategy.
	retrieve func(ctx *astral.Context, target *astral.Identity) <-chan LinkResult

	// migrate replaces migrateSessions where a link hands its sessions on before
	// it closes (closeExcess, retireLink, the connectivity upgrade); nil is migrateSessions.
	// why: a field, so tests observe a migration without a peer to run it with.
	migrate func(oldLinkID astral.Nonce, newLink *Link)

	// retireClock is the time source of retireLink; nil is the real clock.
	// why: a field, so tests move retireLink past retireMax without waiting.
	retireClock wakeClock

	// resolveRemote pulls endpoints from another node.
	// why: a field, so tests answer a pull without a network.
	resolveRemote func(ctx *astral.Context, resolver *astral.Identity, identity *astral.Identity) ([]*nodes.EndpointWithTTL, error)

	// announceDelay is the wait between a local address change and its announcement.
	announceDelay time.Duration
	// announcePending is set while an announcement waits out announceDelay.
	announcePending atomic.Bool
	// lastAnnouncedHash is the fingerprint of the last announced endpoint set, nil before the first announcement.
	lastAnnouncedHash atomic.Pointer[endpointsHash]

	// linkWakeDelay is the wait between a local address change and the wake of the links bound to a removed address.
	linkWakeDelay time.Duration
	// wakeMu guards wakeIPs.
	wakeMu sync.Mutex
	// wakeIPs are the removed addresses that the next wake round covers.
	wakeIPs []ip.IP
	// wakePending is set while a wake round waits out linkWakeDelay.
	wakePending atomic.Bool

	// syncGate runs one endpoint pull per sibling at a time, after a partial link close and in the daily refresh.
	syncGate syncGate

	// siblingRefreshInterval is the period of the RefreshSiblingEndpointsTask run that pulls every linked sibling.
	// why: a field, so tests shorten the 24 h period.
	siblingRefreshInterval time.Duration
	// localRefreshInterval is the period of RefreshSiblingEndpointsTask.
	// why: a field, so tests shorten the 35 min period.
	localRefreshInterval time.Duration
	// fullRefreshDue is the Unix time in nanoseconds of the next refresh run that pulls every linked sibling; 0 is due.
	fullRefreshDue atomic.Int64
	// subnets lists this node's interface subnets; nil when the ip module is not loaded.
	subnets subnetLister
	// fingerprints names this node's network; nil when the ip module is not loaded.
	fingerprints fingerprinter
	// refreshClock is the time source of the refresh task; nil is the real clock.
	// why: a field, so tests move the task past the full-refresh period without waiting.
	refreshClock wakeClock

	// swarm is this node's copy of SwarmState, nil until Run opens it.
	swarm atomic.Pointer[swarmStore]
	// swarmLinkHook replaces onSwarmLink in exchangeSwarmWithLinked.
	// note: a test seam; nil in the running node.
	swarmLinkHook func(peer *astral.Identity)
}

// initContext sets the module context for the node identity id. The context ends when
// mod.cancel runs, which Run arranges when its own context ends.
func (mod *Module) initContext(id *astral.Identity) {
	mod.ctx, mod.cancel = astral.NewContext(nil).WithIdentity(id).IncludeZone(astral.ZoneNetwork).WithCancel()
}

// Run schedules the endpoint-cleanup task, the sibling endpoint refresh and the
// startup endpoint check, and blocks until ctx is cancelled.
func (mod *Module) Run(ctx *astral.Context) error {
	defer mod.linkPool.Close()
	// why: mod.ctx ends with ctx, also while Run waits for the scheduler.
	stop := context.AfterFunc(ctx, mod.cancel)
	defer stop()
	defer mod.cancel()
	<-mod.Deps.Scheduler.Ready()
	if err := mod.openSwarmState(); err != nil {
		mod.log.Error("open swarm state: %v", err)
	} else {
		go mod.runSwarmSync(mod.ctx)
	}
	mod.Scheduler.Schedule(mod.NewCleanupEndpointsTask())
	// why: the first full run comes after one jittered period, as the link-created pull covers the start.
	mod.deferFullRefresh(mod.refreshNow())
	delay, _ := mod.ctx.WithTimeout(jitter(mod.localRefreshInterval, siblingRefreshJitter))
	mod.Scheduler.Schedule(mod.newRefreshSiblingEndpointsTask(), delay)
	// why: a set that changed while the node was off is announced once after the settle wait.
	mod.scheduleEndpointsAnnounce()

	<-ctx.Done()
	return nil
}

func (mod *Module) AddEndpoint(nodeID *astral.Identity, endpoint *nodes.EndpointWithTTL) error {
	return mod.db.AddEndpoint(nodeID, endpoint.Network(), endpoint.Address(), endpointExpiry(endpoint))
}

// endpointExpiry returns when endpoint's TTL ends, and nil when it has none.
func endpointExpiry(endpoint *nodes.EndpointWithTTL) *time.Time {
	if endpoint.TTL == nil {
		return nil
	}
	t := time.Now().UTC().Add(time.Duration(*endpoint.TTL) * time.Second)
	return &t
}

func (mod *Module) RemoveEndpoint(nodeID *astral.Identity, endpoint exonet.Endpoint) error {
	return mod.db.RemoveEndpoint(nodeID, endpoint.Network(), endpoint.Address())
}

// CloseLink closes a link with the given id.
func (mod *Module) CloseLink(id astral.Nonce) error {
	links := mod.linkPool.links.Clone()
	for _, s := range links {
		if s.id == id {
			return s.CloseWithError(errors.New("link closed"))
		}
	}

	return nodesmod.ErrLinkNotFound
}

// CloseLinks closes all open links with the given identity.
func (mod *Module) CloseLinks(identity *astral.Identity) error {
	for _, s := range mod.linkPool.links.Clone() {
		if s.RemoteIdentity().IsEqual(identity) {
			_ = s.CloseWithError(errors.New("node expelled"))
		}
	}

	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return nodesmod.ModuleName
}

func (mod *Module) AddResolver(resolver nodesmod.EndpointResolver) {
	if resolver != nil {
		mod.resolvers.Add(resolver)
	}
}

func (mod *Module) RegisterLinkStrategy(network string, factory nodesmod.StrategyFactory) {
	mod.strategyFactories.Set(network, factory)
}

func (mod *Module) IsLinked(identity *astral.Identity) bool {
	return mod.linkPool.SelectLinkWith(identity) != nil
}

func (mod *Module) GetLinkNegotiator(ch *channel.Channel) *muxLinkNegotiator {
	return &muxLinkNegotiator{
		mod: mod,
		ch:  ch,
	}
}

// getPrivateKey returns the node's private key, loaded on the first call.
//
// note: the key load takes a fresh context for the node identity and not mod.ctx.
// mod.ctx is set in Load (initContext, astrald#557), so an inbound link that
// arrives before Run reads a live mod.ctx either way.
func (mod *Module) getPrivateKey() (*crypto.PrivateKey, error) {
	mod.mu.Lock()
	defer mod.mu.Unlock()

	if mod.privateKey != nil {
		return mod.privateKey, nil
	}

	nodeID := mod.node.Identity()
	key, err := mod.Crypto.PrivateKey(astral.NewContext(nil).WithIdentity(nodeID), modsecp256k1.FromIdentity(nodeID))
	if err != nil {
		return nil, err
	}

	mod.privateKey = key
	return key, nil
}

// getLinkByID returns a link with the given local id or nil if not found.
func (mod *Module) getLinkByID(id astral.Nonce) *Link {
	for _, s := range mod.linkPool.links.Clone() {
		if s.id == id {
			return s
		}
	}
	return nil
}

func (mod *Module) getSessionLink(nonce astral.Nonce) *Link {
	for _, link := range mod.linkPool.links.Clone() {
		_, ok := link.GetMux().sessions.Get(nonce)
		if ok {
			return link
		}
	}

	return nil
}

func (mod *Module) getSession(nonce astral.Nonce) (*session, bool) {
	for _, link := range mod.linkPool.links.Clone() {
		session, ok := link.GetMux().sessions.Get(nonce)
		if ok {
			return session, true
		}
	}

	return nil, false
}

func (mod *Module) reflectLink(link *Link) error {
	if link.Outbound() {
		return nil
	}

	endpoint := link.RemoteEndpoint()
	if endpoint == nil {
		return nil
	}

	if _, ok := endpoint.(*gateway.Endpoint); ok {
		return nil
	}

	err := mod.Objects.Push(mod.ctx, link.RemoteIdentity(),
		&nodes.ObservedEndpointMessage{
			Endpoint: endpoint,
		})
	if err != nil {
		mod.log.Errorv(2, "Objects.Push(%v, %v): %v", link.RemoteIdentity(), endpoint, err)
		return err
	}

	mod.log.Logv(2, "reflected endpoint %v to %v", endpoint, link.RemoteIdentity())
	return nil
}
