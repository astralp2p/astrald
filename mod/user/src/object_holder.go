package user

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

var _ objects.Holder = &Module{}

func (mod *Module) HoldObject(objectID *astral.ObjectID) (hold bool) {
	return mod.db.assetExists(objectID)
}
