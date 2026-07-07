package apphost

import "github.com/cryptopunkscc/astral-go/astral"

// HoldObject reports whether the given object is currently held by any app.
// Returns true on DB error to prevent premature garbage collection (fail-closed).
func (mod *Module) HoldObject(objectID *astral.ObjectID) bool {
	held, err := mod.db.ObjectHeld(objectID)
	if err != nil {
		mod.log.Error("object hold lookup failed: %v", err)
		return true
	}
	return held
}
