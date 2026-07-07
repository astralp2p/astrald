package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/scheduler"
)

type ScheduledTask struct {
	task     scheduler.Task
	state    scheduler.State
	done     chan struct{}
	ctx      *astral.Context
	cancel   context.CancelCauseFunc
	doneOnce *sync.Once
	err      error
	mu       sync.RWMutex

	// timestamps
	scheduledAt time.Time
	runAt       time.Time
	doneAt      time.Time
}

func NewScheduledTask(task scheduler.Task) *ScheduledTask {
	return &ScheduledTask{
		task:        task,
		scheduledAt: time.Now(),
		done:        make(chan struct{}),
		doneOnce:    &sync.Once{},
	}
}

func (task *ScheduledTask) Run(ctx *astral.Context) (err error) {
	// prepare the execution
	err = task.prepare(ctx)
	if err != nil {
		return err
	}

	// run the task
	err = task.task.Run(task.ctx)

	// store results
	task.mu.Lock()
	defer task.mu.Unlock()

	defer task.closeDone()

	task.err = err
	task.doneAt = time.Now()
	task.state = scheduler.StateDone

	return err
}

// prepare prepares the task for running by creating its context and setting its state
func (task *ScheduledTask) prepare(ctx *astral.Context) error {
	task.mu.Lock()
	defer task.mu.Unlock()

	if task.state != scheduler.StateScheduled {
		return scheduler.ErrInvalidState
	}

	if ctx == nil {
		return scheduler.ErrContextIsNil
	}

	task.ctx, task.cancel = ctx.WithCancelCause()
	task.runAt = time.Now()
	task.state = scheduler.StateRunning

	return nil
}

// CancelWithError cancels the task with the given error.
func (task *ScheduledTask) CancelWithError(err error) {
	task.mu.Lock()
	defer task.mu.Unlock()

	// handle cancel depending on the current state
	switch task.state {
	case scheduler.StateDone:
		// ignore

	case scheduler.StateScheduled:
		task.err = err
		task.state = scheduler.StateDone
		task.closeDone()

	case scheduler.StateRunning:
		task.cancel(err)

	default:
		panic("invalid state")
	}
}

// Cancel cancels the task with context.Canceled.
func (task *ScheduledTask) Cancel() {
	task.CancelWithError(context.Canceled)
}

// closeDone closes the done channel
func (task *ScheduledTask) closeDone() {
	task.doneOnce.Do(func() {
		close(task.done)
	})
}

// Task returns the scheduled task
func (task *ScheduledTask) Task() scheduler.Task {
	return task.task
}

func (task *ScheduledTask) Done() <-chan struct{} {
	return task.done
}

// Err returns the error returned by the task. Should only be called after the task is done.
func (task *ScheduledTask) Err() error {
	return task.err
}

func (task ScheduledTask) ScheduledAt() time.Time {
	return task.scheduledAt
}

func (task *ScheduledTask) State() scheduler.State {
	return task.state
}

func (task *ScheduledTask) String() string {
	return task.task.String()
}

func (task *ScheduledTask) setState(state scheduler.State) {
	task.state = state
}

func (task *ScheduledTask) runTime() time.Duration {
	return task.doneAt.Sub(task.runAt)
}
