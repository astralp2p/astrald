package archives

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeSeeObjects grants read access to an archive entry by recursively
// verifying that the actor can read the parent archive that contains it.
func (mod *Module) AuthorizeSeeObjects(ctx *astral.Context, action *auth.SeeObjectsAction) bool {
	// note: SeeObjects also covers ops that name no object — enumeration, blueprints,
	// the repository list. An archive says nothing about those, so it grants nothing.
	if action.ObjectID == nil {
		return false
	}

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
		return mod.Auth.Authorize(ctx, &auth.SeeObjectsAction{
			Action:   auth.NewAction(action.Actor()),
			ObjectID: zipID,
		})
	}

	return false
}
