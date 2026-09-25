package messaging

import (
	"context"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// Archive puts one of the owner's messages away, or puts it back, and reports
// whether this call is the one that moved it.
func (mod *Module) Archive(_ context.Context, owner *astral.Identity, ref messaging.MessageRef, undo bool) (bool, error) {
	if !mod.hosts(owner) {
		return false, errNotParticipant
	}

	r := messageRef{Box: string(ref.Box), ID: ref.ID}
	if err := r.validate(); err != nil {
		return false, err
	}

	return mod.archiveMessage(owner, r, undo)
}

// archiveMessage puts one message away, or puts it back, and reports whether
// this call is the one that moved it.
//
// why RowsAffected is the answer: admission and write are one statement, where a
// lookup then a write would race. The same zero means "already there" and "not
// yours" on purpose — separating them would tell a caller whether an id it does
// not hold exists at all.
func (mod *Module) archiveMessage(owner *astral.Identity, ref messageRef, undo bool) (bool, error) {
	move := mod.db.Archive
	if undo {
		move = mod.db.Unarchive
	}

	n, err := move(owner, ref.Box, ref.ID)
	if err != nil {
		return false, err
	}

	// why undo wakes and archive does not: clearing archived_at puts the row
	// back into the wait set with no insert to signal on, so it is the one
	// statement besides a delivery that adds to what a park is watching.
	if undo && n == 1 {
		mod.waiters.wake(owner)
	}

	return n == 1, nil
}
