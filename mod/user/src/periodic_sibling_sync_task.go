package user

import (
	"math/rand/v2"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// note: this task is an interim safety net for a sibling that missed a relay
// contract for any reason; Universal Object Delivery supersedes it
// (https://wiki.satforge.dev/doc/universal-object-delivery-Qo2EsUffMl).
// It is removable as a unit: delete this file, the SiblingSync* fields of
// FileConfig, and the one start line in Module.Run.

// defaultSiblingSyncInterval is the period of the sibling sync when user.yaml
// sets none.
const defaultSiblingSyncInterval = 6 * time.Hour

// siblingSyncJitter is the share of the interval a tick can be delayed by.
// why: nodes that restart together would otherwise sync their siblings together.
const siblingSyncJitter = 0.1

// runPeriodicSiblingSync runs a SyncNodesTask with every linked sibling every
// sibling sync interval until ctx ends. It returns at once when the sync is
// disabled: sibling_sync_disabled is set or sibling_sync_interval is negative.
func (mod *Module) runPeriodicSiblingSync(ctx *astral.Context) {
	if mod.file.SiblingSyncDisabled || mod.file.SiblingSyncInterval < 0 {
		return
	}

	mod.periodicSiblingSync(ctx, mod.siblingSyncInterval(), func(ctx *astral.Context, sib *astral.Identity) {
		_ = mod.NewSyncNodesTask(sib).Run(ctx)
	})
}

// siblingSyncInterval returns the configured sibling sync period, or the
// default when user.yaml sets zero.
func (mod *Module) siblingSyncInterval() time.Duration {
	if mod.file.SiblingSyncInterval <= 0 {
		return defaultSiblingSyncInterval
	}
	return mod.file.SiblingSyncInterval
}

// jitteredSiblingSyncDelay returns interval plus a random share of it below
// siblingSyncJitter.
func jitteredSiblingSyncDelay(interval time.Duration) time.Duration {
	return interval + time.Duration(rand.Float64()*siblingSyncJitter*float64(interval))
}

// periodicSiblingSync waits one jittered interval, then
// starts run for every linked sibling whose previous run has finished, and
// repeats until ctx ends.
// why a skip and not syncGate: a gate queues a rerun behind the running sync;
// a sibling that stalls a sync for hours needs no second sync behind it, the
// next tick covers it.
// why a goroutine per sibling: a stalled sibling must not delay the others.
func (mod *Module) periodicSiblingSync(ctx *astral.Context, interval time.Duration, run func(*astral.Context, *astral.Identity)) {
	var busy siblingSyncBusy

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitteredSiblingSyncDelay(interval)):
		}

		for _, sib := range mod.getSiblings() {
			if !busy.start(sib) {
				mod.log.Logv(1, "periodic sync with %v still runs, skipped", sib)
				continue
			}
			go func() {
				defer busy.done(sib)
				run(ctx, sib)
			}()
		}
	}
}

// siblingSyncBusy holds the siblings whose periodic sync runs.
type siblingSyncBusy struct {
	mu  sync.Mutex
	set map[string]struct{}
}

// start marks sib busy and reports false when it already is.
func (b *siblingSyncBusy) start(sib *astral.Identity) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	key := sib.String()
	if _, running := b.set[key]; running {
		return false
	}
	if b.set == nil {
		b.set = make(map[string]struct{})
	}
	b.set[key] = struct{}{}
	return true
}

// done marks sib idle.
func (b *siblingSyncBusy) done(sib *astral.Identity) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.set, sib.String())
}
