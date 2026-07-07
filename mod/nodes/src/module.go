package nodes

import (
	"errors"
	"time"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/crypto"
	"github.com/cryptopunkscc/astrald/mod/gateway"
	"github.com/cryptopunkscc/astrald/mod/nodes"
	modsecp256k1 "github.com/cryptopunkscc/astrald/mod/secp256k1"
	"github.com/cryptopunkscc/astrald/resources"
)

const DefaultWorkerCount = 8
const infoPrefix = "node1"
const featureMux2 = "mux2"
const defaultPingTimeout = time.Second * 30
const activeInterval = 1 * time.Second
const pingJitter = 1 * time.Second

var _ nodes.Module = &Module{}

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
	resolvers  sig.Set[nodes.EndpointResolver]

	observedEndpoints sig.Map[string, ObservedEndpoint] // key is IP string

	linkPool *LinkPool

	strategyFactories sig.Map[string, nodes.StrategyFactory]
	upgraders         sig.Map[string, *sig.Switch]

	searchCache sig.Map[string, *astral.Identity]

	privateKey *crypto.PrivateKey
}

// Run schedules the endpoint-cleanup task and blocks until ctx is cancelled.
func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx.IncludeZone(astral.ZoneNetwork)
	<-mod.Deps.Scheduler.Ready()
	mod.Scheduler.Schedule(mod.NewCleanupEndpointsTask())

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

	return nodes.ErrLinkNotFound
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
	return nodes.ModuleName
}

func (mod *Module) AddResolver(resolver nodes.EndpointResolver) {
	if resolver != nil {
		mod.resolvers.Add(resolver)
	}
}

func (mod *Module) RegisterLinkStrategy(network string, factory nodes.StrategyFactory) {
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

func (mod *Module) getPrivateKey() (_ *crypto.PrivateKey, err error) {
	if mod.privateKey != nil {
		return mod.privateKey, nil
	}

	mod.privateKey, err = mod.Crypto.PrivateKey(mod.ctx, modsecp256k1.FromIdentity(mod.ctx.Identity()))
	if err != nil {
		return nil, err
	}

	return mod.privateKey, nil
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
