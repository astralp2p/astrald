package user

import (
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
// Module embeds it. The store takes the module's own SwarmView through its
// constructor and calls CheckSwarmEntry on every merge.
type SwarmState interface {
	// AddSwarmEntry offers obj to the set, as a local write or a merge from a
	// peer. obj must be an entry: user.EntryKindOf(obj) reports true. An
	// entry already stored is a no-op returning nil, decided before any
	// check. Otherwise it runs SwarmView.CheckSwarmEntry:
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
	// poll it directly.
	SwarmHead() uint64
}
