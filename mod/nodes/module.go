package nodes

import (
	"context"
	"github.com/astralp2p/astral-go/api/nodes"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/astral"
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

	// DefaultBufferSize is the default buffer size for session I/O.
	DefaultBufferSize = 4 * 1024 * 1024
	MaxDataFrameSize  = 8192
)

// Module manages encrypted links between nodes: establishing them, resolving
// peer endpoints, and tracking liveness.
type Module interface {
	EstablishInboundLink(ctx context.Context, conn exonetmod.Conn) error
	EstablishOutboundLink(ctx context.Context, remoteID *astral.Identity, conn exonetmod.Conn) (Link, error)

	AddEndpoint(*astral.Identity, *nodes.EndpointWithTTL) error
	RemoveEndpoint(*astral.Identity, exonet.Endpoint) error

	// UpdateNodeEndpoints pulls identity's endpoints from resolver, stores them,
	// and returns how many of them were new.
	UpdateNodeEndpoints(ctx *astral.Context, resolver *astral.Identity, identity *astral.Identity) (int, error)
	ResolveEndpoints(*astral.Context, *astral.Identity) (<-chan *nodes.EndpointWithTTL, error)
	AddResolver(resolver EndpointResolver)

	IsLinked(*astral.Identity) bool

	// CloseLinks closes all open links with the given identity.
	CloseLinks(identity *astral.Identity) error

	// AddLinkAdmitter registers a rule that every dial, inbound handshake and
	// added link consults (astral-docs protocols/nodes/README.md, Link admission).
	AddLinkAdmitter(LinkAdmitter)
	// EnforceLinkPolicy closes every link whose peer an admitter now refuses.
	EnforceLinkPolicy()

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

// LinkAdmitter decides whether this node may hold a link with remote.
// note: AdmitLink runs on every inbound handshake, so it never blocks.
type LinkAdmitter interface {
	// AdmitLink returns nil to admit a link with remote, or why it is refused.
	AdmitLink(remote *astral.Identity) error
}

// EndpointResolver resolves the network endpoints at which an identity can be reached.
type EndpointResolver interface {
	ResolveEndpoints(*astral.Context, *astral.Identity) (<-chan *nodes.EndpointWithTTL, error)
}

// LinkStrategy drives one approach to establishing a link (e.g. direct, NAT, Tor).
// Join adds a caller to the strategy's running round, starting one when none runs,
// and returns the caller's idempotent release. The round runs on a module-owned
// context with a budget; the round is cancelled once every joined caller has
// released, or when the budget ends. A caller also leaves when ctx ends.
// Done closes once the round has finished, whether or not it produced a link.
type LinkStrategy interface {
	Name() string
	Join(ctx *astral.Context) (release func())
	Done() <-chan struct{}
}

type StrategyFactory interface {
	Build(target *astral.Identity) LinkStrategy
}
