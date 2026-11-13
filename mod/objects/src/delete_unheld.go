package objects

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

// DeleteUnheld deletes a full ObjectID from every tracked leaf below repo unless a holder holds it or the leaf
// committed it after since. It returns objects.ErrObjectInUse when a leaf kept it for one of those, and
// objects.ErrNotFound when no leaf had it. A leaf that cannot delete is skipped.
//
// why: the check and the delete run under the leaf's lock, as a purge decides, so a commit of the same content
// or a holder that a commit precedes cannot fall between them.
func (mod *Module) DeleteUnheld(ctx *astral.Context, repo objects.Repository, objectID *astral.ObjectID, since time.Time) error {
	// why: the empty object is never stored.
	if objects.IsEmptyObject(objectID) {
		return nil
	}

	var deleted, kept bool
	for _, leaf := range mod.trackedLeaves(repo) {
		err := mod.deleteUnheldIn(ctx, leaf, objectID, since)
		switch {
		case err == nil:
			deleted = true
		case errors.Is(err, objects.ErrObjectInUse):
			kept = true
		case errors.Is(err, objects.ErrNotFound), errors.Is(err, errors.ErrUnsupported):
		default:
			return err
		}
	}

	switch {
	case kept:
		return objects.ErrObjectInUse
	case deleted:
		return nil
	}

	return objects.ErrNotFound
}

// deleteUnheldIn deletes the object from one leaf under its lock unless a holder holds it or the leaf committed it
// after since.
//
// note: tracking keeps whole seconds, so a commit in the second of since counts as before it.
// note: an object without a row is deleted when nothing holds it; only a failed tracking write leaves one.
func (mod *Module) deleteUnheldIn(ctx *astral.Context, leaf *trackedRepository, objectID *astral.ObjectID, since time.Time) error {
	leaf.mu.Lock()
	defer leaf.mu.Unlock()

	row, err := mod.db.findTracking(leaf.name, objectID)
	if err != nil {
		return err
	}
	if row != nil && row.CommittedAt > since.Unix() {
		return objects.ErrObjectInUse
	}

	if len(mod.Holders(objectID)) > 0 {
		return objects.ErrObjectInUse
	}

	return leaf.deleteLocked(ctx, objectID)
}
