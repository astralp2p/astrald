package auth

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

var _ objects.Holder = &Module{}

// HoldObject reports whether the object is referenced by an active contract.
// Returns true on DB error to avoid premature eviction of live contract data.
func (mod *Module) HoldObject(objectID *astral.ObjectID) bool {
	held, err := mod.db.activeContractExists(objectID)
	if err != nil {
		mod.log.Error("object hold lookup failed: %v", err)
		return true
	}
	return held
}
