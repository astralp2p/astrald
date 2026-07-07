package scheduler

import (
	"sync"
	"sync/atomic"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/mod/scheduler"
	"github.com/cryptopunkscc/astrald/resources"
)

// Ensure Module struct implements the public scheduler.Module interface
var _ scheduler.Module = (*Module)(nil)

// Module is the concrete implementation of the scheduler module.
type Module struct {
	Deps

	ctx    *astral.Context
	node   astral.Node
	log    *log.Logger
	assets resources.Resources
	queue  sig.Set[scheduler.ScheduledTask]
	ready  chan struct{}
}

func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx
	close(mod.ready)

	<-ctx.Done()
	return nil
}

// Schedule schedules a task to be executed once all dependencies are done.
func (mod *Module) Schedule(task scheduler.Task, deps ...scheduler.Done) (_ scheduler.ScheduledTask, err error) {
	// check if the scheduler is running
	if !mod.isRunning() {
		mod.log.Errorv(2, "cannot schedule (not running): %v", task)
		return nil, scheduler.ErrNotRunning
	}

	// check if the task is nil
	if task == nil {
		return nil, scheduler.ErrTaskIsNil
	}

	// create a scheduled task
	sTask := NewScheduledTask(task)

	// add the task to the queue
	// err is always nil, because duplicates are impossible here
	_ = mod.queue.Add(sTask)

	// spawn the task runner goroutine
	go func() {
		var releasers sig.Set[scheduler.Releaser] // releasable deps
		var canceled atomic.Bool

		// make sure to clean up at the end
		defer func() {
			// remove from queue
			mod.queue.Remove(sTask)

			// release all releasable dependencies
			for _, dep := range releasers.Clone() {
				dep.Release()
			}
		}()

		// wait for all dependencies asynchronously
		var wg sync.WaitGroup
		for _, dep := range deps {
			wg.Add(1)
			go func() {
				defer wg.Done()

				select {
				case <-sTask.Done():
					canceled.Store(true)

				case <-dep.Done():
					// store for release if needed
					if r, ok := dep.(scheduler.Releaser); ok {
						releasers.Add(r)
					}
				}
			}()
		}
		wg.Wait()

		// bail if the context was canceled
		if canceled.Load() {
			return
		}

		// run the task within the context of the scheduler module
		err = sTask.Run(mod.ctx)

		// log on error
		if err != nil {
			mod.log.Errorv(2, "task %v: %v", task, err)
		}
	}()

	return sTask, nil
}

// isRunning returns true if the scheduler module is running.
func (mod *Module) isRunning() bool {
	if mod.ctx == nil {
		return false
	}
	select {
	case <-mod.ctx.Done():
		return false
	default:
	}
	return true
}

// Ready returns a channel that is closed once Run has stored the module context.
func (mod *Module) Ready() <-chan struct{} {
	return mod.ready
}

func (mod *Module) String() string {
	return scheduler.ModuleName
}
