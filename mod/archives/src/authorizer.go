package archives

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

// AuthorizeObjectsRead grants read access to an archive entry by recursively
// verifying that the actor can read the parent archive that contains it.
func (mod *Module) AuthorizeObjectsRead(ctx *astral.Context, action *objects.ReadObjectAction) bool {
	var rows []*dbEntry

	var err = mod.db.
		Unscoped().
		Preload("Parent").
		Where("object_id = ?", action.ObjectID).
		Find(&rows).Error
	if err != nil {
		return false
	}

	for _, row := range rows {
		if row.Parent == nil {
			mod.log.Errorv(1, "db: entry for %v references an invalid parent", action.ObjectID)
			continue
		}

		zipID := row.Parent.ObjectID

		// sanity check
		if zipID.IsEqual(action.ObjectID) {
			continue
		}

		// Recursive check: can the actor read the parent archive?
		return mod.Auth.Authorize(ctx, &objects.ReadObjectAction{
			Action:   auth.NewAction(action.Actor()),
			ObjectID: zipID,
		})
	}

	return false
}
