package vfs

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// sweepPage is the number of notes the sweeper reads at once.
const sweepPage = 256

// runDirSweeper deletes replaced directory objects every sweep interval until ctx ends.
func (mod *Module) runDirSweeper(ctx *astral.Context) {
	for {
		t := mod.clock.NewTimer(mod.config.DirectorySweepInterval)

		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C():
		}

		if _, err := mod.sweepReplaced(ctx, mod.clock.Now()); err != nil {
			mod.log.Error("sweep replaced directories: %v", err)
		}
	}
}

// sweepReplaced deletes each noted directory object older than the grace time that nothing holds, and returns how
// many notes ended in a delete.
//
// note: a note is read in pages loaded before any holder or repository is asked, so no cursor holds the one
// database connection.
func (mod *Module) sweepReplaced(ctx *astral.Context, now time.Time) (deleted int, err error) {
	cutoff := now.Add(-mod.config.DirectoryGraceTime).UnixNano()

	var after int64
	for {
		notes, err := mod.db.listReplacedBefore(cutoff, after, sweepPage)
		if err != nil {
			return deleted, err
		}

		for _, note := range notes {
			ok, err := mod.sweepNote(ctx, note)
			if err != nil {
				return deleted, err
			}
			if ok {
				deleted++
			}
		}

		if len(notes) < sweepPage {
			return deleted, nil
		}
		after = notes[len(notes)-1].ID
	}
}

// sweepNote deletes the directory object of one note unless a holder holds it, and reports whether it was deleted.
// A note that a store holds is kept for the next sweep. An object already gone counts as deleted.
//
// why: a holder covers a directory that is current again, one another folder or VFS has, and one an app holds.
// why: the note is claimed as it was read, so a note made again after its page was read keeps its grace time.
func (mod *Module) sweepNote(ctx *astral.Context, note dbReplacedDirectory) (bool, error) {
	id := *note.ObjectID
	if !mod.gate.tryBeginDelete(id) {
		return false, nil
	}
	defer mod.gate.endDelete(id)

	if len(mod.Objects.Holders(&id)) > 0 {
		_, err := mod.db.claimReplaced(note)
		return false, err
	}

	repo := mod.Objects.WriteDefault()
	if repo == nil {
		return false, errNoWriteRepository
	}

	claimed, err := mod.db.claimReplaced(note)
	if err != nil || !claimed {
		return false, err
	}

	return mod.deleteClaimed(ctx, repo, note)
}

// deleteClaimed deletes the directory object of a claimed note, and reports whether it was deleted. The objects
// module checks the holders again and the commit time against the note under the lock of each leaf.
//
// why: a writer of the same bytes that is not a directory store takes no gate, so the check before the delete
// runs where its commit runs.
// note: an object that could not be deleted loses its note and is left to the daily purge.
func (mod *Module) deleteClaimed(ctx *astral.Context, repo objectsmod.Repository, note dbReplacedDirectory) (bool, error) {
	id := *note.ObjectID

	err := mod.Objects.DeleteUnheld(ctx, repo, &id, time.Unix(0, note.NotedAt))
	switch {
	case err == nil, errors.Is(err, objectsmod.ErrNotFound):
		return true, nil
	case errors.Is(err, objectsmod.ErrObjectInUse):
		// why: a holder or a commit came after the note; noted again now, the object gets a new grace time.
		return false, mod.db.noteReplaced([]astral.ObjectID{id}, mod.clock.Now().UnixNano())
	case ctx.Err() != nil:
		// why: the note is made again as it was read, so the next sweep takes it.
		return false, errors.Join(ctx.Err(), mod.db.noteReplaced([]astral.ObjectID{id}, note.NotedAt))
	}

	mod.log.Error("delete replaced directory %v: %v", id, err)

	return false, nil
}
