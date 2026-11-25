package nodes

import (
	"context"
	"github.com/astralp2p/astral-go/api/nodes"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/user"
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
	// A pull about the node's own identity queries and stores nothing and returns 0.
	UpdateNodeEndpoints(ctx *astral.Context, resolver *astral.Identity, identity *astral.Identity) (int, error)
	// RefreshMemberEndpoints pulls subject's endpoints from subject itself and
	// forwards the notice one hop to the other linked siblings.
	RefreshMemberEndpoints(subject *astral.Identity)
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

// SwarmState is this node's copy of the swarm's grow-only set of signed
// entries: requests, Approvals, Declines, contracts and expels. mod/nodes
// stores it under swarmstate/<kind>/<entry ID> in the node's own VFS and syncs
// it between nodes; a merge is a union, and nothing is ever overwritten or
// deleted. What an entry means is decided by user.SwarmView (mod/user).
//
// An entry ID is astral.ResolveObjectID of the signed entry exactly as stored
// (e.g. *user.SignedApproval, signatures included), never of its unsigned
// body. Every reference uses it: user.Approval.RequestID,
// user.Decline.RequestID, auth.Contract.ApprovalID and the IDs user.SwarmView
// takes, so each resolves with SwarmEntry.
//
// It is declared apart from Module: nothing implements it until G1, and G7
// embeds it into Module. Until then G1's implementation takes its
// user.SwarmView through its constructor and stays unwired; G7 passes
// mod/user once Module embeds SwarmView.
type SwarmState interface {
	// AddSwarmEntry offers obj to the set, as a local write or a merge from a
	// peer. obj must be an entry: user.EntryKindOf(obj) reports true. An
	// entry already stored is a no-op returning nil, decided before any
	// check. Otherwise it runs user.SwarmView.CheckSwarmEntry:
	//   - EntryAccept: obj is stored, SwarmHead grows by one, obj is passed on
	//     to peers, and it returns nil;
	//   - EntryWait: obj is held unstored and checked again after the next
	//     merge; it returns ErrSwarmEntryHeld. The held set is in memory and
	//     bounded; when full, the oldest held entry is dropped first;
	//   - EntryDrop: obj is neither stored nor passed on; it returns an error
	//     wrapping ErrSwarmEntryDropped.
	// It returns another error for a non-entry and when the store fails.
	AddSwarmEntry(ctx *astral.Context, obj astral.Object) error

	// SwarmEntry returns the stored entry with entry ID id, decoded as its
	// signed type (e.g. *user.SignedApproval). It returns an error when no
	// stored entry has that id; held (EntryWait) entries are not returned.
	SwarmEntry(id *astral.ObjectID) (astral.Object, error)

	// SwarmEntries returns the entry IDs of every stored entry of kind, in no
	// defined order. It returns an error for an invalid kind and when the
	// store cannot be read. Held (EntryWait) entries are not listed.
	SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error)

	// SwarmHead returns the number of stored entries. It only grows, so a
	// reader that sees the same value as its last poll has nothing new to read.
	// It describes this node's copy and is not synced. Readers in any module
	// poll it directly; mod/user's watcher is internal to mod/user.
	SwarmHead() uint64
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
// Join adds a caller to the strategy's running round, starting one when none runs.
// It returns the joined round's done channel and the caller's idempotent release.
// The round runs on a module-owned context with a budget; the round is cancelled
// once every joined caller has released, or when the budget ends. A caller also
// leaves when ctx ends. done closes once the joined round has finished, whether
// or not it produced a link; for Tor it closes when the foreground phase ends.
type LinkStrategy interface {
	Name() string
	Join(ctx *astral.Context) (done <-chan struct{}, release func())
}

type StrategyFactory interface {
	Build(target *astral.Identity) LinkStrategy
}
