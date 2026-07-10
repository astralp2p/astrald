package crypto

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

var _ objects.Holder = &Module{}

// HoldObject reports whether the object is an indexed key.
// Fails safe: a lookup error returns true so the object is not dropped.
func (mod *Module) HoldObject(objectID *astral.ObjectID) bool {
	held, err := mod.db.isKeyIndexed(objectID)
	if err != nil {
		mod.log.Error("object hold lookup failed: %v", err)
		return true
	}
	return held
}
