package swarmset

import (
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// Checker decides whether an entry may enter the set: it is the entry check. The set calls it, under its merge
// lock, for every entry that is not stored yet: a local write, a peer's push, a pulled entry, a held entry
// checked again, and every stored entry when the set opens.
type Checker interface {
	// Check returns obj's verdict, reading the stored entries only through r.
	Check(obj astral.Object, r Reader) user.EntryVerdict
}

// Reader is the read-only view of the stored entries a Checker gets. It is taken under the merge lock, so it
// cannot write, and it reads only what the set already holds in memory.
type Reader interface {
	// Entry returns the stored entry with entry ID id.
	Entry(id *astral.ObjectID) (astral.Object, bool)
	// Snapshot returns the stored entries as read now, with root as the root key: what an offer's check reads
	// besides the request it names (expels, Declines, the request's contracts).
	Snapshot(root *astral.Identity) *Snapshot
}
