package vfs

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// errDirStore reports that at least one directory of a plan was not stored.
var errDirStore = errors.New("directory not stored")

// errNoWriteRepository reports that the objects module has no default write repository.
var errNoWriteRepository = errors.New("no default write repository")

// storeOutcome is what storing one plan did.
type storeOutcome struct {
	Charge     uint64 // each distinct directory stored, at least the minimum charge
	Failed     bool   // a directory was not stored
	Lost       bool   // a stored directory was not written back, because its folder changed
	RootCached bool   // the root of the VFS has a cached ObjectID after the write-back
	Done       bool   // the plan reached the folder it was made for
	Planned    int    // the directories the plan held
}

// lockCompute takes the compute lock of one VFS and returns the function that releases it.
//
// The computes of one VFS, in the background and on request, run one at a time. The lock is taken before the lock
// of the VFS and never while waiting for tokens.
// note: the order is compute lock, then the lock of the VFS, then a transaction.
func (mod *Module) lockCompute(id *astral.Identity) (unlock func()) {
	mu, _ := mod.computeLocks.Set(id.String(), &sync.Mutex{})
	mu.Lock()

	return mu.Unlock
}

// storePlanLogged stores a plan and logs what failed.
func (mod *Module) storePlanLogged(plan *dirPlan) storeOutcome {
	out, err := mod.storePlan(plan)
	if err != nil {
		mod.log.Error("store the directories of %v: %v", plan.VFS, err)
	}

	return out
}

// storePlan stores the directories of a plan in the default write repository and writes back the ObjectID of each
// folder whose directory and every planned directory below it were stored, and that did not change since the plan.
// A failed store keeps the VFS pending for a retry after the longest wait.
//
// note: the caller holds the compute lock of the VFS, and neither its lock nor a transaction.
func (mod *Module) storePlan(plan *dirPlan) (out storeOutcome, err error) {
	if len(plan.Dirs) == 0 {
		return out, nil
	}

	ids := plan.distinctIDs()
	for _, id := range ids {
		mod.gate.beginStore(id)
	}
	// why: the store window lasts to the end of the write-back, so the sweeper never deletes an ID a row is about
	// to take.
	defer func() {
		for _, id := range ids {
			mod.gate.endStore(id)
		}
	}()

	stored := mod.storeDirs(plan, &out)
	err = mod.writeBack(plan, stored, &out)

	if out.Failed {
		mod.sched.noteFailure(plan.VFS, mod.clock.Now())
		err = errors.Join(err, errDirStore)
	}

	return out, err
}

// distinctIDs returns the ObjectIDs of the planned directories, each once.
func (plan *dirPlan) distinctIDs() (ids []astral.ObjectID) {
	for _, d := range plan.Dirs {
		if !slices.Contains(ids, d.ID) {
			ids = append(ids, d.ID)
		}
	}

	return ids
}

// storeDirs stores the planned directories, folders below first, and reports which are stored. A directory with a
// planned folder below that was not stored is skipped.
func (mod *Module) storeDirs(plan *dirPlan, out *storeOutcome) []bool {
	repo := mod.Objects.WriteDefault()
	stored := make([]bool, len(plan.Dirs))
	done := map[astral.ObjectID]bool{}

	for i := range plan.Dirs {
		d := &plan.Dirs[i]
		if !allSet(stored, d.Children) {
			continue
		}
		if done[d.ID] {
			stored[i] = true
			continue
		}

		if err := mod.storeDirectory(repo, d); err != nil {
			mod.log.Error("store directory %v of %v: %v", d.ID, plan.VFS, err)
			out.Failed = true
			continue
		}

		done[d.ID], stored[i] = true, true
		out.Charge += max(uint64(len(d.Bytes)), mod.config.DirectoryMinCharge)
	}

	return stored
}

// storeDirectory writes one directory to a repository and commits it. A commit under another ObjectID fails.
func (mod *Module) storeDirectory(repo objectsmod.Repository, d *plannedDir) error {
	if repo == nil {
		return errNoWriteRepository
	}

	w, err := repo.Create(mod.ctx, &objectsmod.CreateOpts{Alloc: len(d.Bytes)})
	if err != nil {
		return err
	}
	defer w.Discard()

	if _, err = w.Write(d.Bytes); err != nil {
		return err
	}

	id, err := w.Commit()
	switch {
	case err != nil:
		return err
	case *id != d.ID:
		// fixme: the object committed under the other ObjectID is left to the daily purge.
		return fmt.Errorf("bug: committed as %v", id)
	}

	return nil
}

// writeBack caches the ObjectIDs of the stored directories in one transaction under the lock of the VFS. A stored
// directory that no folder took is noted as replaced. A VFS whose root is cached is no longer pending.
//
// why: the pending state is dropped under the lock of the VFS, after the commit, so a change that commits later is
// noted after it.
func (mod *Module) writeBack(plan *dirPlan, stored []bool, out *storeOutcome) error {
	now := mod.clock.Now().UnixNano()
	var written []bool

	err := mod.runLockedTxThen(plan.VFS, func(tx *DB) (err error) {
		if written, err = tx.writeBackPlan(plan, stored); err != nil {
			return err
		}
		if err = tx.noteReplaced(unusedIDs(plan, stored, written), now); err != nil {
			return err
		}
		out.RootCached, err = tx.isRootCached(plan.VFS)

		return err
	}, func() {
		if out.RootCached {
			mod.sched.settle(plan.VFS)
		}
	})
	if err != nil {
		// why: the notes rolled back with the write-back, so the stored directories are noted on their own.
		if noteErr := mod.db.noteReplaced(unusedIDs(plan, stored, nil), now); noteErr != nil {
			mod.log.Error("note the directories of %v: %v", plan.VFS, noteErr)
		}
		return err
	}

	for i := range stored {
		out.Lost = out.Lost || (stored[i] && !written[i])
	}

	return nil
}

// writeBackPlan caches each stored directory whose planned folders below were written back, folders below first,
// and reports which rows took their ObjectID.
func (tx *DB) writeBackPlan(plan *dirPlan, stored []bool) ([]bool, error) {
	written := make([]bool, len(plan.Dirs))

	for i, d := range plan.Dirs {
		if !stored[i] || !allSet(written, d.Children) {
			continue
		}

		ok, err := tx.setFolderIDIfUnchanged(d.Row, d.Gen, d.ID)
		if err != nil {
			return nil, err
		}
		written[i] = ok
	}

	return written, nil
}

// unusedIDs returns the stored ObjectIDs that no written row took, each once. A nil written means none.
func unusedIDs(plan *dirPlan, stored, written []bool) (ids []astral.ObjectID) {
	used := map[astral.ObjectID]bool{}
	for i, d := range plan.Dirs {
		if written != nil && written[i] {
			used[d.ID] = true
		}
	}

	for i, d := range plan.Dirs {
		if stored[i] && !used[d.ID] {
			used[d.ID] = true
			ids = append(ids, d.ID)
		}
	}

	return ids
}

// allSet reports whether every index is set.
func allSet(set []bool, indexes []int) bool {
	for _, i := range indexes {
		if !set[i] {
			return false
		}
	}

	return true
}
