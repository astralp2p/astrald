package objects

import (
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
)

// purgeTaskInterval is how often the purge task purges the group local.
const purgeTaskInterval = 24 * time.Hour

// runPurgeTask purges the group local with the window untouched once every interval until ctx ends.
// A window of 0 turns the task off.
// note: the first run comes one interval after start (VFS 6, §11 "objects: purge").
// note: operator decision 2026-11-13: a window at or below zero turns the task off.
func (mod *Module) runPurgeTask(ctx *astral.Context, interval time.Duration, untouched time.Duration) {
	if untouched <= 0 {
		mod.log.Logv(1, "purge task: off")
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mod.purgeLocal(ctx, untouched)
		}
	}
}

// purgeLocal purges the group local once with the window untouched and logs the result.
func (mod *Module) purgeLocal(ctx *astral.Context, untouched time.Duration) {
	repo := mod.GetRepository(objects.RepoLocal)
	if repo == nil {
		mod.log.Error("purge task: repository %v not found", objects.RepoLocal)
		return
	}

	purged, errPtr := mod.purgeRepository(ctx, repo, untouched, false)
	var count int
	for range purged {
		count++
	}

	if *errPtr != nil {
		mod.log.Error("purge task: purge %v: %v", objects.RepoLocal, *errPtr)
	}
	mod.log.Logv(1, "purge task: purged %v objects from %v", count, objects.RepoLocal)
}
