package user

import (
	"iter"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

const ModuleName = "user"
const DBPrefix = "users__"

// UserKeyPath is the BIP-32 path user.bootstrap derives the user's key along.
//
// why the Coldcard path: one seed derives one user on a Coldcard and in
// software (operator decision, 2026-10-21).
const UserKeyPath = "m/1791'/0'/0'/0/0"

const (
	// MinimalContractLength is the shortest remaining validity of a node
	// contract that user.accept_membership accepts.
	MinimalContractLength = time.Hour

	// DefaultContractValidity is the validity of a node contract that
	// user.new_node_contract builds without a duration.
	DefaultContractValidity = 365 * 24 * time.Hour
)

type Module interface {
	// Ready returns a channel that is closed once the module has applied the
	// initial active contract and is fully initialized.
	Ready() <-chan struct{}
	Identity() *astral.Identity
	// LocalSwarm returns the identities of swarm members reachable on this
	// device, excluding remote-only members.
	LocalSwarm() (list []*astral.Identity)
	// AdmitRelayContract is the one admission rule of a relay-for contract:
	// its subject is this node or a local swarm member, both signatures
	// verify, and it has not expired. A non-nil sender, the node that
	// delivered it, must be a member too. It answers nil for any other kind.
	AdmitRelayContract(sender *astral.Identity, sc *auth.SignedContract) error
	// FetchRelayContracts asks every linked sibling at once for the relay
	// contracts issuer issued, and yields each answered contract that passes
	// AdmitRelayContract with the answering sibling as sender, with that
	// sibling. It ends when every sibling has answered, ctx ends, or the
	// consumer stops. It asks nothing outside the network zone.
	FetchRelayContracts(ctx *astral.Context, issuer *astral.Identity) iter.Seq2[*astral.Identity, *auth.SignedContract]
	NewMaintainLinkTask(target *astral.Identity) MaintainLinkTask
	NewSyncNodesTask(remoteIdentity *astral.Identity) SyncNodesTask
	// PushToLocalSwarm broadcasts obj to every local swarm member except the
	// node itself using ctx; delivery is best-effort and failures are silently ignored.
	PushToLocalSwarm(ctx *astral.Context, obj astral.Object)
	// PushToSiblings sends obj to every linked sibling, each push on its own
	// and bounded; delivery is best-effort.
	PushToSiblings(ctx *astral.Context, obj astral.Object)
	// Expel permanently bans nodeID from the swarm. Only the active contract's
	// issuer may expel and the ban is irreversible.
	Expel(ctx *astral.Context, nodeID *astral.Identity) (*user.SignedExpulsion, error)
	// WasOwned reports whether a user contract has been active on this node,
	// including one that has since expired or been expelled.
	WasOwned() bool
	// SetPolicyDelegate stores d as the delegate of the named user policy
	// (swarm_join, swarm_invite or contract_accept), or clears it when d is
	// nil. Another name is an error.
	SetPolicyDelegate(ctx *astral.Context, name string, d *policy.Delegate) error
}
