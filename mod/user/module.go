package user

import (
	"iter"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
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
// through SwarmState and does no other I/O. Every answer derives from
// the entries stored at the time of the call, and every ID it takes or returns
// is an entry ID (see SwarmState). Stored entries passed the entry check
// (see SwarmState), so reads trust their signatures and that each answer's signer was
// entitled to answer its request (user.MayAnswer); they apply what changes
// after merge: Timeouts, expiry, Declines and expels. An expel beats
// everything signed by or for the expelled identity.
//
// The root key is the user identity this node is claimed by; an unclaimed node
// is its own root. Implementations read time from an injected clock that
// defaults to time.Now, so tests and fakes agree on it.
//
// Module embeds it. A "carried out, or not yet" view and the change feed stay
// inside mod/user; other modules poll SwarmState.SwarmHead or react to the
// SwarmState events (astral-go api/user), and call these methods.
type SwarmView interface {
	// RequestStatus returns the status of the request with entry ID req, in
	// precedence order:
	//   - user.RequestFulfilled: a stored contract with both signatures names
	//     req as its RequestID and is given to the identity the request names
	//     (ContractRequest.Identity, JoinRequest.Joiner) or to its filer
	//     (ContractRequest.FiledBy). An ExpelRequest is never fulfilled;
	//   - user.RequestDeclined: a Decline names req; or the request was filed
	//     by, carried by (JoinRequest.Carrier) or names
	//     (ContractRequest.Identity, JoinRequest.Joiner) an expelled identity.
	//     A Decline closes the request and takes back no contract;
	//   - user.RequestApproved: req is an ExpelRequest and an Approval names
	//     it, or the root key's user.SignedExpulsion names its Subject. An
	//     Approval counts whenever it was given. Only an ExpelRequest is
	//     approved;
	//   - user.RequestExpired: the request's Timeout has passed;
	//   - user.RequestOffered: an offer names req: a contract only its issuer
	//     signed, given to the request's filer, that has not expired;
	//   - user.RequestPending: otherwise.
	// An Approval, Decline or offer signed by an expelled identity does not
	// count. A req that names no stored request, including one held
	// (EntryWait), answers user.RequestPending: a caller that needs to know
	// first checks SwarmState.SwarmEntry, and a filer applies its own copy's
	// Expired.
	RequestStatus(req *astral.ObjectID) user.RequestStatus

	// Expelled reports whether a stored user.SignedExpulsion names id as its
	// Subject.
	Expelled(id *astral.Identity) bool

	// ContractsOf returns the stored contracts whose RequestID is req, in no
	// defined order, whatever their signatures, expiry or the expels of their
	// parties: offers, their completed forms and the contracts that carry the
	// request out. A host or carrier calls it before signing, so a request is
	// carried out once.
	ContractsOf(req *astral.ObjectID) []*auth.SignedContract

	// Membership returns id's membership: a stored membership contract
	// (user.ContractTier reports a tier) whose Subject is id, that has not
	// expired, holds its tier's permit alone (user.TierOnly), and whose issuer
	// may issue its tier:
	//   - the root key, at any tier; a membership at
	//     user.TierPrivilegedMember holds only this way (D2);
	//   - or a member whose own membership holds and whose tier issues that
	//     tier: Privileged issues Member and Trusted, Trusted issues Member.
	// Its RequestID adds nothing. Both the issuer's and the subject's
	// signatures are required. It returns
	// false when none holds or id, or the contract's issuer, is expelled. When
	// several hold, it returns the one of the highest tier, and among those
	// the one that expires last.
	Membership(id *astral.Identity) (*user.Membership, bool)

	// MayAnswer reports whether signer may answer the stored request with
	// entry ID req: the root key answers every request, any other signer by
	// the tier of its own membership (user.MayAnswer). It reports false for a
	// req that names no stored request.
	MayAnswer(signer *astral.Identity, req *astral.ObjectID) bool
}

type Module interface {
	SwarmState
	SwarmView
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
	// IsBanned reports whether id is a node the active user banned: a stored
	// Expulsion names it (Expelled). It reports false with no active contract,
	// for the user and for this node.
	IsBanned(id *astral.Identity) bool
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
}
