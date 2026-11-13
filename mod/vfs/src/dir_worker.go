package vfs

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// dirPassBudget bounds the directory bytes one pass of a background compute plans. See Module.passBudget.
//
// why: a pass holds the lock of the VFS and the node's one database connection while it plans, so a large cleared
// VFS is computed in passes, and requests run between them.
const dirPassBudget = 1 << 20

// runDirWorker runs the background compute until ctx ends: one VFS at a time, the most overdue first.
func (mod *Module) runDirWorker(ctx *astral.Context) {
	for ctx.Err() == nil {
		if ran, next := mod.dirWorkerStep(); !ran {
			mod.waitDirWork(ctx, next)
		}
	}
}

// waitDirWork waits until next, a new pending VFS or the end of ctx. A zero next waits without a timer.
func (mod *Module) waitDirWork(ctx *astral.Context, next time.Time) {
	var fire <-chan time.Time
	if !next.IsZero() {
		t := mod.clock.NewTimer(next.Sub(mod.clock.Now()))
		defer t.Stop()
		fire = t.C()
	}

	select {
	case <-ctx.Done():
	case <-fire:
	case <-mod.sched.wake:
	}
}

// dirWorkerStep computes the most overdue VFS that its bucket and the node pace can pay for. It reports whether
// it computed one, and else when to look again; zero when nothing is pending.
//
// why: while the node pace is not ready nothing runs, so the most overdue VFS keeps its claim on the node.
// note: the tokens are checked before the compute lock is taken, so the lock is never held while waiting.
func (mod *Module) dirWorkerStep() (ran bool, next time.Time) {
	now := mod.clock.Now()

	ready, next := mod.sched.dueList(now)
	if len(ready) == 0 {
		return false, next
	}

	if at := mod.sched.nodeReadyAt(); at.After(now) {
		return false, earliest(next, at)
	}

	for _, d := range ready {
		at := mod.checkTokens(d, now)
		if at.IsZero() || at.After(now) {
			next = earliest(next, at)
			continue
		}

		mod.runBackgroundCompute(d.ID, now)

		return true, time.Time{}
	}

	return false, next
}

// checkTokens returns when the bucket of a due VFS can pay for its compute, estimating the cost when it is not
// known. A VFS that needs no compute is dropped, and zero is returned.
func (mod *Module) checkTokens(d dueVFS, now time.Time) time.Time {
	if d.ReadyAt.After(now) {
		return d.ReadyAt
	}

	cost := d.Cost
	if cost == 0 {
		var err error
		if cost, err = mod.estimateCost(d.ID); err != nil {
			mod.log.Error("estimate the directories of %v: %v", d.ID, err)
			mod.sched.noteFailure(d.ID, now)
			return now.Add(mod.config.DirectoryLongestWait)
		}
		if cost == 0 {
			return time.Time{}
		}
	}

	return mod.sched.bucketReadyAt(d, cost, now)
}

// estimateCost estimates the compute of a VFS under its lock. A VFS with no cleared folder costs 0 and is no
// longer pending.
func (mod *Module) estimateCost(id *astral.Identity) (cost uint64, err error) {
	err = mod.runLockedTxThen(id, func(tx *DB) (err error) {
		cost, err = tx.estimateDirCost(id, mod.config.DirectoryMinCharge)
		return err
	}, func() {
		if cost == 0 {
			mod.sched.settle(id)
		}
	})

	return cost, err
}

// runBackgroundCompute computes one VFS and charges what it stored.
func (mod *Module) runBackgroundCompute(id *astral.Identity, start time.Time) {
	charged, err := mod.computeVFS(id)
	if err != nil && !errors.Is(err, errDirStore) {
		mod.log.Error("compute the directories of %v: %v", id, err)
		mod.sched.noteFailure(id, mod.clock.Now())
	}

	mod.sched.afterCompute(id, charged, start, mod.clock.Now())
}

// computeVFS computes the root ObjectID of a VFS in passes, and stores and writes back each pass. It stops when
// the root is planned, when a folder changed during a pass, or when a store failed. It returns the charge of all
// passes.
func (mod *Module) computeVFS(id *astral.Identity) (charged uint64, err error) {
	for {
		out, err := mod.computePass(id)
		charged += out.Charge

		switch {
		case err != nil:
			return charged, err
		case out.Done, out.Lost, out.Planned == 0:
			return charged, nil
		}
	}
}

// computePass plans one pass of a VFS under its locks, then stores and writes it back. The compute lock is held
// for the pass only, so a request waits for one pass at most.
func (mod *Module) computePass(id *astral.Identity) (storeOutcome, error) {
	unlock := mod.lockCompute(id)
	defer unlock()

	var plan *dirPlan

	err := mod.runLockedTx(id, func(tx *DB) error {
		vol, err := tx.findVolume(id)
		if err != nil {
			return err
		}

		root, err := tx.findEntry(vol.RootID)
		if err != nil {
			return err
		}

		plan, err = planFolder(tx, id, root, mod.passBudget)

		return err
	})
	if err != nil {
		return storeOutcome{}, err
	}

	out, err := mod.storePlan(plan)
	out.Done, out.Planned = plan.Done, len(plan.Dirs)

	return out, err
}
