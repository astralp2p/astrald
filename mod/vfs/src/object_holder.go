package vfs

import (
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

var _ objectsmod.Holder = &Module{}

// HoldObject reports whether any file of any VFS has the ObjectID. The objects module never purges a held object.
//
// A cached ObjectID of a folder names no stored object and is not held.
// A database error answers true, so a failing lookup never lets an object be purged.
//
// note: the objects module finds the module as a holder by scanning the modules it loads, see
// mod/objects/src/deps.go.
func (mod *Module) HoldObject(objectID *astral.ObjectID) bool {
	held, err := mod.db.hasFileWithObject(objectID)
	if err != nil {
		mod.log.Error("object hold lookup failed: %v", err)
		return true
	}

	return held
}
