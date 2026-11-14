package objects

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/objects"
)

// purgeFloor is the default of Config.PurgeFloor, the shortest window a purge accepts.
// note: the floor covers commits and reads. It closes the gap between the commit of a file's content and the write of its entry.
// note: the operator accepted the floor for now; it may change later (VFS 6, §11 "objects: purge").
const purgeFloor = time.Hour

// purgePageSize is how many candidates one listing query returns.
const purgePageSize = 256

// purgeWindow returns untouched raised to floor.
func purgeWindow(untouched, floor time.Duration) time.Duration {
	return max(untouched, floor)
}

// purgeRun is one purge: the cutoff of its window, whether it is dry, and where it answers purged ObjectIDs.
// note: the rules are in VFS 6, §11 "objects: purge": https://wiki.satforge.dev/doc/vfs-6-specification-BxBnrSeTfT
type purgeRun struct {
	mod    *Module
	cutoff int64 // seconds since the Unix epoch; an object last touched before it is a candidate
	dry    bool
	out    chan<- *astral.ObjectID
}

// purgeRepository purges every tracked leaf of repo: repo itself when it is a leaf, each leaf below it once when it is a group.
// It deletes an object only when nothing holds it and it was not touched within untouched.
// A dry purge answers the same ObjectIDs and deletes nothing.
func (mod *Module) purgeRepository(ctx *astral.Context, repo objects.Repository, untouched time.Duration, dry bool) (<-chan *astral.ObjectID, *error) {
	out := make(chan *astral.ObjectID)
	errPtr := new(error)

	run := &purgeRun{
		mod:    mod,
		cutoff: time.Now().Add(-purgeWindow(untouched, mod.config.PurgeFloor)).Unix(),
		dry:    dry,
		out:    out,
	}

	go func() {
		defer close(out)

		for _, leaf := range mod.trackedLeaves(repo) {
			err := run.purgeLeaf(ctx, leaf)
			if err != nil {
				*errPtr = err
				return
			}
		}
	}()

	return out, errPtr
}

// leafRef names a tracked leaf and the repository it wraps.
type leafRef struct {
	name string
	repo objects.Repository
}

// trackedLeaves returns the tracked leaves of repo in member order. A leaf below several members is listed once.
func (mod *Module) trackedLeaves(repo objects.Repository) []leafRef {
	var leaves []leafRef
	seen := map[objects.Repository]bool{}

	var walk func(repo objects.Repository)
	walk = func(repo objects.Repository) {
		// why: a group can name a group that names it back.
		if seen[repo] {
			return
		}
		seen[repo] = true

		if group, ok := repo.(*RepoGroup); ok {
			for _, member := range group.members() {
				walk(member)
			}
			return
		}

		name := mod.getRepoName(repo)
		if inner, ok := mod.tracker.leaf(name); ok {
			leaves = append(leaves, leafRef{name: name, repo: inner})
		}
	}
	walk(repo)

	return leaves
}

// purgeLeaf purges one leaf: it lists the candidates, then decides each one on current data.
func (run *purgeRun) purgeLeaf(ctx *astral.Context, leaf leafRef) error {
	run.flushJournal()

	var after *untouchedCursor
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		rows, next, err := run.mod.db.listUntouched(leaf.name, run.cutoff, after, purgePageSize)
		if err != nil {
			return err
		}

		for _, row := range rows {
			purged, err := run.decide(ctx, leaf, row.ObjectID)
			if err != nil {
				return err
			}
			if !purged {
				continue
			}

			err = sig.Send(ctx, run.out, row.ObjectID)
			if err != nil {
				return err
			}
		}

		if next == nil {
			return nil
		}
		after = next
	}
}

// decide takes the leaf's lock, checks the candidate again on current data, and deletes it unless the run is dry.
// It reports whether the candidate is purged.
// why: a read or a commit between the listing and this turn moves the last touch, so the listed row is stale.
func (run *purgeRun) decide(ctx *astral.Context, leaf leafRef, objectID *astral.ObjectID) (bool, error) {
	mu := run.mod.tracker.leafLock(leaf.name)
	mu.Lock()
	defer mu.Unlock()

	// why: an unwritten read can make an object look untouched, so a candidate is skipped when the journal cannot be written.
	if !run.flushJournal() {
		return false, nil
	}

	row, err := run.mod.db.findTracking(leaf.name, objectID)
	if err != nil {
		return false, err
	}
	if row == nil || row.touch() >= run.cutoff {
		return false, nil
	}

	// note: a held object is kept in every repository that has it, whatever its times say.
	if len(run.mod.Holders(objectID)) > 0 {
		return false, nil
	}

	if run.dry {
		return run.wouldDelete(ctx, leaf, objectID), nil
	}

	return run.delete(ctx, leaf, objectID)
}

// wouldDelete reports whether a run that is not dry deletes the candidate.
// why: a run that is not dry drops the row of an object missing from the leaf and answers nothing for it.
func (run *purgeRun) wouldDelete(ctx *astral.Context, leaf leafRef, objectID *astral.ObjectID) bool {
	has, err := leaf.repo.Contains(ctx, objectID)
	return err == nil && has
}

// delete deletes the candidate through the tracker, which removes its row. The caller holds the leaf's lock.
// A delete that answers not found drops the row.
func (run *purgeRun) delete(ctx *astral.Context, leaf leafRef, objectID *astral.ObjectID) (bool, error) {
	err := run.mod.tracker.deleteLocked(ctx, leaf.name, leaf.repo, objectID)
	switch {
	case err == nil:
		return true, nil

	case errors.Is(err, objects.ErrNotFound):
		run.mod.tracker.dropRow(leaf.name, objectID)
		return false, nil

	case errors.Is(err, errors.ErrUnsupported):
		return false, nil

	default:
		return false, err
	}
}

// flushJournal writes the reads journal and reports whether it succeeded.
func (run *purgeRun) flushJournal() bool {
	err := run.mod.tracker.flush()
	if err != nil {
		run.mod.log.Error("object reads journal: purge flush: %v", err)
		return false
	}
	return true
}
