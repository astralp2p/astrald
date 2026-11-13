package vfs

import (
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

var _ objectsmod.Holder = &Module{}

// HoldObject reports whether any file of any VFS has the ObjectID as its content, or any folder as its cached
// directory. The objects module never purges a held object.
//
// A replaced directory is in no row and is not held; the sweeper deletes it (see sweepReplaced).
// A database error answers true, so a failing lookup never lets an object be purged.
// note: it never takes a lock of the module, because a purge asks it while it holds a repository's lock.
//
// note: the objects module finds the module as a holder by scanning the modules it loads, see
// mod/objects/src/deps.go.
func (mod *Module) HoldObject(objectID *astral.ObjectID) bool {
	held, err := mod.db.hasEntryWithObject(objectID)
	if err != nil {
		mod.log.Error("object hold lookup failed: %v", err)
		return true
	}

	return held
}
