package nodes

import (
	"context"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/exonet"
)

const (
	StrategyBasic = "basic"
	StrategyTor   = "tor"
	StrategyNAT   = "nat"
)

const (
	ModuleName     = "nodes"
	DBPrefix       = "nodes__"
	ActionRelayFor = "mod.nodes.relay_for_action" // equals RelayForAction{}.ObjectType()

	CleanupGrace    = 30 * 24 * time.Hour
	CleanupInterval = 24 * time.Hour

	// query extra keys
	ExtraCallerProof   = "caller_proof"
	ExtraRelayVia      = "relay_via"
	ExtraRoutingPolicy = "routing_policy"

	// MethodResolveEndpoints is the query route for resolving endpoints of a node.
	MethodResolveEndpoints = "nodes.resolve_endpoints"

	// MethodMigrateSession is the query route for session migration.
	MethodMigrateSession = "nodes.migrate_session"

	// DefaultBufferSize is the default buffer size for session I/O.
	DefaultBufferSize = 4 * 1024 * 1024
	MaxDataFrameSize  = 8192
)

// Module manages encrypted links between nodes: establishing them, resolving
// peer endpoints, and tracking liveness.
type Module interface {
	EstablishInboundLink(ctx context.Context, conn exonet.Conn) error
	EstablishOutboundLink(ctx context.Context, remoteID *astral.Identity, conn exonet.Conn) (Link, error)

	AddEndpoint(*astral.Identity, *EndpointWithTTL) error
	RemoveEndpoint(*astral.Identity, exonet.Endpoint) error

	UpdateNodeEndpoints(ctx *astral.Context, resolver *astral.Identity, identity *astral.Identity) error
	ResolveEndpoints(*astral.Context, *astral.Identity) (<-chan *EndpointWithTTL, error)
	AddResolver(resolver EndpointResolver)

	IsLinked(*astral.Identity) bool

	// CloseLinks closes all open links with the given identity.
	CloseLinks(identity *astral.Identity) error

	NewCreateLinkTask(target *astral.Identity, endpoint exonet.Endpoint) CreateLinkTask
	NewEnsureLinkTask(target *astral.Identity, strategies []string, networks []string, forceNew bool) EnsureLinkTask
	NewCleanupEndpointsTask() CleanupEndpointsTask
}

// Link is an encrypted communication channel between two identities that is capable of routing queries
type Link interface {
	astral.Router
	// SetRouter(router astral.Router)
	LocalIdentity() *astral.Identity
	RemoteIdentity() *astral.Identity
	Close() error
	Done() <-chan struct{}
}

// EndpointResolver resolves the network endpoints at which an identity can be reached.
type EndpointResolver interface {
	ResolveEndpoints(*astral.Context, *astral.Identity) (<-chan *EndpointWithTTL, error)
}

// LinkStrategy drives one approach to establishing a link (e.g. direct, NAT, Tor);
// Done closes once the strategy has finished, whether or not it produced a link.
type LinkStrategy interface {
	Name() string
	Signal(ctx *astral.Context)
	Done() <-chan struct{}
}

type StrategyFactory interface {
	Build(target *astral.Identity) LinkStrategy
}
