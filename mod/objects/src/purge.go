package objects

import (
	"errors"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/objects"
)

func (mod *Module) purgeRepository(ctx *astral.Context, repo objects.Repository) (<-chan *astral.ObjectID, *error) {
	out := make(chan *astral.ObjectID)
	errPtr := new(error)

	go func() {
		defer close(out)

		// flush pending reads so the read order reflects the latest accesses
		err := mod.objectsReadsJournal.Flush()
		if err != nil {
			mod.log.Error("object reads journal: purge flush: %v", err)
		}

		// why: a row names its leaf, so a stale row is dropped only when the purged repository is that leaf.
		var leaf string
		if tracked, ok := repo.(*trackedRepository); ok {
			leaf = tracked.name
		}

		// purge in touch order: least recently touched objects first, paged via keyset cursor
		var after *readCursor
		for {
			select {
			case <-ctx.Done():
				*errPtr = ctx.Err()
				return
			default:
			}

			rows, next, err := mod.db.ListReadOldest(leaf, after, 256)
			if err != nil {
				*errPtr = err
				return
			}

			for _, row := range rows {
				id := row.ObjectID
				if len(mod.Holders(id)) > 0 {
					continue
				}

				err := repo.Delete(ctx, id)
				switch {
				case err == nil:
					// note: the tracked leaf removed its row on delete.
					err = sig.Send(ctx, out, id)
					if err != nil {
						*errPtr = err
						return
					}

				case errors.Is(err, objects.ErrNotFound):
					if leaf == "" {
						continue
					}
					derr := mod.db.deleteTracking(leaf, id)
					if derr != nil {
						mod.log.Error("purge: drop stale cache row %v: %v", id, derr)
					}
					continue
				case errors.Is(err, errors.ErrUnsupported):
					continue
				default:
					*errPtr = err
					return
				}
			}

			if next == nil {
				break
			}
			after = next
		}
	}()

	return out, errPtr
}
