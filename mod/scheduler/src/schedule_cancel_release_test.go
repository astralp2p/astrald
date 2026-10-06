package scheduler

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// TestScheduleCanceledTaskReleasesPoolLock covers Schedule: a task canceled while it
// waits on a LockPool dependency releases the pool items once the dependency acquires them.
func TestScheduleCanceledTaskReleasesPoolLock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mod := &Module{log: &log.Logger{}, ctx: astral.NewContext(ctx)}

	pool := sig.NewPool()
	pool.Add("r", 1)
	pool.Lock("r") // the holder

	dep := scheduler.LockPool(pool, "r")
	sTask, err := mod.Schedule(scheduler.Func("never", func(*astral.Context) error {
		t.Error("canceled task ran")
		return nil
	}), dep)
	if err != nil {
		t.Fatalf("Schedule returned %v; want nil", err)
	}
	sTask.Cancel()

	// the runner removes the task from the queue only after it stopped waiting on deps
	for mod.queue.Contains(sTask) {
		runtime.Gosched()
	}

	// the holder releases r, so the dependency acquires it
	pool.Unlock("r")
	<-dep.Done()

	// the canceled task must not keep r locked
	locked := make(chan struct{})
	go func() {
		pool.Lock("r")
		close(locked)
	}()

	select {
	case <-locked:
		pool.Unlock("r")
	case <-time.After(5 * time.Second):
		t.Fatal("r still locked after the canceled task's dependency acquired it: pool lock leaked")
	}
}
