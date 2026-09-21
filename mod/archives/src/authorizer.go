package archives

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeSeeObjects grants read access to an archive entry when the actor can
// read any indexed archive that contains it, checked recursively.
//
// why any parent and not the first: an entry is indexed once per archive that
// holds it and the lookup carries no ORDER BY, so deciding on one row makes the
// answer depend on an unspecified row order. Any parent also grants nothing new —
// an actor who may read the parent archive reads the entry's bytes out of it
// directly (object_opener.go).
//
// why a false is not a denial: mod/auth composes handlers by OR
// (mod/auth/src/authorize.go, authorizeHandlers), so declining here leaves every
// other handler free to grant.
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
		if mod.Auth.Authorize(ctx, &auth.SeeObjectsAction{
			Action:   auth.NewAction(action.Actor()),
			ObjectID: zipID,
		}) {
			return true
		}
	}

	return false
}
