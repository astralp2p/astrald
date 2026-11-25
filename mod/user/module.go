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

// SwarmView reads what SwarmState's entries mean (mod/user). It reads the set
// through nodes.SwarmState and does no other I/O. Every answer derives from
// the entries stored at the time of the call. Stored entries passed
// CheckSwarmEntry on merge, so the reads do not verify signatures again; they
// apply what changes after merge: Timeouts, expiry, Declines and expels. An
// expel beats everything signed by or for the expelled identity.
//
// It is declared apart from Module: nothing implements it until G2 (reads) and
// G3 (CheckSwarmEntry), and G7 embeds it into Module.
type SwarmView interface {
	// RequestStatus returns the status of the request with ObjectID req, in
	// precedence order:
	//   - user.RequestDeclined: a Decline names req, even after a
	//     contract carried out an Approval of it;
	//   - user.RequestApproved: an Approval names req;
	//   - user.RequestExpired: the request's Timeout has passed;
	//   - user.RequestPending: otherwise.
	// An Approval or Decline signed by an expelled identity does not count.
	// A req that names no stored request answers user.RequestPending.
	RequestStatus(req *astral.ObjectID) user.RequestStatus

	// Expelled reports whether a stored user.SignedExpulsion names id as its
	// Subject.
	Expelled(id *astral.Identity) bool

	// ApprovalsTouching returns the stored Approvals that node acts on: those
	// of requests node filed (ContractRequest, ExpelRequest), carried in
	// (JoinRequest.Carrier), or that name node (ContractRequest.Identity,
	// JoinRequest.Joiner, ExpelRequest.Subject). It includes Approvals whose
	// request is since declined; RequestStatus tells them apart. The order is
	// not defined.
	ApprovalsTouching(node *astral.Identity) []*user.SignedApproval

	// Membership returns id's membership: a stored membership contract
	// (user.ContractTier reports a tier) whose Subject is id, that has not
	// expired, and whose link holds:
	//   - signed by the user key, with or without an ApprovalID; or
	//   - signed by a JoinRequest's Carrier, carrying out (ApprovalID) an
	//     Approval of that JoinRequest, which names id as Joiner at the same
	//     tier and is not declined.
	// It returns false when none holds or id, or the contract's issuer, is
	// expelled. When several hold, it returns the one that expires last.
	Membership(id *astral.Identity) (*user.Membership, bool)

	// CheckSwarmEntry decides whether obj may enter SwarmState. mod/nodes
	// calls it before storing any entry. It returns:
	//   - user.EntryDrop: obj is not an entry, a signature does not verify,
	//     or its signer's tier may not write that kind of entry (D5, D7, D10);
	//   - user.EntryWait: the signer's membership is not stored yet;
	//   - user.EntryAccept: otherwise.
	// An entry that was valid when it arrived stays valid; expels apply when
	// entries are read, not here.
	CheckSwarmEntry(obj astral.Object) user.EntryVerdict
}

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
	// FetchRelayContractsReport is FetchRelayContracts with a completeness
	// report. complete, read after the sequence has ended, is true only when
	// every asked sibling answered without error before ctx ended and the
	// consumer did not stop early; no linked sibling is complete (a real
	// "no"). It is false when nothing was asked (outside the network zone).
	FetchRelayContractsReport(ctx *astral.Context, issuer *astral.Identity) (seq iter.Seq2[*astral.Identity, *auth.SignedContract], complete func() bool)
	// IsSwarmMember reports whether id is a current member of the local swarm.
	// It reports false on an unclaimed node and when the swarm cannot be read.
	IsSwarmMember(id *astral.Identity) bool
	NewMaintainLinkTask(target *astral.Identity) MaintainLinkTask
	NewSyncNodesTask(remoteIdentity *astral.Identity) SyncNodesTask
	// PushToLocalSwarm broadcasts obj to every local swarm member except the
	// node itself using ctx; delivery is best-effort and failures are silently ignored.
	PushToLocalSwarm(ctx *astral.Context, obj astral.Object)
	// PushToSiblings sends obj to every linked sibling, each push on its own
	// and bounded; delivery is best-effort.
	PushToSiblings(ctx *astral.Context, obj astral.Object)
	// PushToSiblingsRetry sends obj to every linked sibling like
	// PushToSiblings, and sends it again, at most 3 times on a bounded
	// backoff, to a sibling the push did not reach. A sibling's retries stop
	// when it answers (accepts or refuses), leaves the linked siblings, or
	// ctx ends.
	PushToSiblingsRetry(ctx *astral.Context, obj astral.Object)
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
