package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// SwarmState is this node's copy of the swarm's grow-only set of signed
// entries: requests, Approvals, Declines, contracts and expels. mod/user
// stores it under swarmstate/<kind>/<entry ID> in the node's own VFS and syncs
// it between nodes; a merge is a union, and nothing is ever overwritten or
// deleted. What an entry means is decided by SwarmView.
//
// An entry ID is astral.ResolveObjectID of the signed entry exactly as stored
// (e.g. *user.SignedApproval, signatures included), never of its unsigned
// body. Every reference uses it: user.Approval.RequestID,
// user.Decline.RequestID, auth.Contract.ApprovalID and the IDs SwarmView
// takes, so each resolves with SwarmEntry.
//
// Every write ends in the one merge, which runs the entry check: signatures,
// Timeouts, tiers, root-only kinds, and that an answer names a stored request
// its signer may answer. Every stored entry passed it, including the entries
// loaded when the store opens. A write first checks whether this node may do
// it and returns ErrNotAllowed early; the merge check has the final say, and
// an entry it drops returns an error wrapping ErrSwarmEntryDropped. An entry
// it holds (it waits for what it needs) counts as written: it is checked again
// after every merge, and stored once what it needs arrives. Only the sync ops
// add entries other nodes signed.
//
// Before the store is open SwarmEntries returns nil, SwarmHead returns 0, and
// every other method returns an error.
//
// Module embeds it.
type SwarmState interface {
	// FileContractRequest signs, with this node's key, a ContractRequest by
	// this node for permits for app, with the default Timeout, and adds it.
	FileContractRequest(ctx *astral.Context, app *astral.Identity, permits []*auth.Permit) (*user.SignedContractRequest, error)

	// FileJoinRequest adds req, a JoinRequest its joiner signed that names
	// this node as its Carrier. It returns req's entry ID.
	FileJoinRequest(ctx *astral.Context, req *user.SignedJoinRequest) (*astral.ObjectID, error)

	// FileExpelRequest signs, with this node's key, an ExpelRequest by this
	// node naming subject, and adds it. Only a Trusted or Privileged member
	// files one (D10).
	FileExpelRequest(ctx *astral.Context, subject *astral.Identity) (*user.SignedExpelRequest, error)

	// Approve signs, with this node's key, an Approval of the stored request
	// with entry ID request, and adds it. This node must be the root key or
	// hold a tier that may answer the request (SwarmView.MayAnswer).
	Approve(ctx *astral.Context, request *astral.ObjectID) (*user.SignedApproval, error)

	// Decline signs, with this node's key, a Decline of the stored request
	// with entry ID request, and adds it, under the rule of Approve.
	Decline(ctx *astral.Context, request *astral.ObjectID) (*user.SignedDecline, error)

	// AddContract adds sc, a contract its issuer signed: a membership, or a
	// contract carrying out an Approval.
	AddContract(ctx *astral.Context, sc *auth.SignedContract) error

	// AddExpulsion adds e, an Expulsion the user key signed (D7).
	AddExpulsion(ctx *astral.Context, e *user.SignedExpulsion) error

	// SwarmEntry returns the stored entry with entry ID id, decoded as its
	// signed type (e.g. *user.SignedApproval). It returns an error when no
	// stored entry has that id; held entries are not returned.
	SwarmEntry(id *astral.ObjectID) (astral.Object, error)

	// SwarmEntries returns the entry IDs of every stored entry of kind, in no
	// defined order. It returns an error for an invalid kind and when the
	// store cannot be read. Held entries are not listed.
	SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error)

	// SwarmHead returns the number of stored entries. It only grows, so a
	// reader that sees the same value as its last poll has nothing new to read.
	// It describes this node's copy and is not synced. Readers in any module
	// poll it directly.
	SwarmHead() uint64
}

// PrivilegedBy reports whether m is a PrivilegedMembership the root key root
// issued: the one check that a node acts for the user (D2).
func PrivilegedBy(m *user.Membership, root *astral.Identity) bool {
	return m != nil && !root.IsZero() && m.Tier == user.TierPrivilegedMember && m.Issuer.IsEqual(root)
}
