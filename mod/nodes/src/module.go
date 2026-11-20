package nodes

import (
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
	ctx    *astral.Context
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

	// siblingRefreshInterval is the period of RefreshSiblingEndpointsTask.
	// why: a field, so tests shorten the 24 h period.
	siblingRefreshInterval time.Duration
}

// Run schedules the endpoint-cleanup task, the sibling endpoint refresh and the
// startup endpoint check, and blocks until ctx is cancelled.
func (mod *Module) Run(ctx *astral.Context) error {
	defer mod.linkPool.Close()
	mod.ctx = ctx.IncludeZone(astral.ZoneNetwork)
	<-mod.Deps.Scheduler.Ready()
	mod.Scheduler.Schedule(mod.NewCleanupEndpointsTask())
	// why: the first run comes after one jittered period, as the link-created pull covers the start.
	delay, _ := mod.ctx.WithTimeout(jitter(mod.siblingRefreshInterval, siblingRefreshJitter))
	mod.Scheduler.Schedule(mod.newRefreshSiblingEndpointsTask(), delay)
	// why: a set that changed while the node was off is announced once after the settle wait.
	mod.scheduleEndpointsAnnounce()

	<-ctx.Done()
	return nil
}

func (mod *Module) AddEndpoint(nodeID *astral.Identity, endpoint *nodes.EndpointWithTTL) error {
	var expiresAt *time.Time
	if endpoint.TTL != nil {
		t := time.Now().UTC().Add(time.Duration(*endpoint.TTL) * time.Second)
		expiresAt = &t
	}

	return mod.db.AddEndpoint(nodeID, endpoint.Network(), endpoint.Address(), expiresAt)
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
// why not mod.ctx: Run sets it, and a transport accepts links once it runs, so the
// first inbound link can arrive before Run.
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
