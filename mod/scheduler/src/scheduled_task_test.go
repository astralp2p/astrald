package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/scheduler"
)

var errTaskFailed = errors.New("task failed")

func countingFunc(name string, fn func(*astral.Context) error) (*scheduler.FuncAdapter, *atomic.Uint64) {
	var calls atomic.Uint64
	return scheduler.Func(name, func(ctx *astral.Context) error {
		calls.Add(1)
		return fn(ctx)
	}), &calls
}

func waitDone(t *testing.T, task *ScheduledTask) {
	t.Helper()
	select {
	case <-task.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() did not close within 2s")
	}
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestNewScheduledTaskStartsScheduled(t *testing.T) {
	adapter := scheduler.Func("t", func(*astral.Context) error { return nil })

	task := NewScheduledTask(adapter)

	if got := task.State(); got != scheduler.StateScheduled {
		t.Fatalf("State() = %v, want %v", got, scheduler.StateScheduled)
	}
	if got := task.String(); got != "t" {
		t.Fatalf("String() = %q, want %q", got, "t")
	}
	if got := task.Task(); got != adapter {
		t.Fatalf("Task() = %v, want the adapter it was built from", got)
	}
	if isClosed(task.Done()) {
		t.Fatal("Done() is closed before the task ran")
	}
	if task.ScheduledAt().IsZero() {
		t.Fatal("ScheduledAt() is zero, want the creation instant")
	}
}

func TestScheduledTaskRunWithNilContextIsRefused(t *testing.T) {
	adapter, calls := countingFunc("t", func(*astral.Context) error { return nil })
	task := NewScheduledTask(adapter)

	if err := task.Run(nil); !errors.Is(err, scheduler.ErrContextIsNil) {
		t.Fatalf("Run(nil) = %v, want %v", err, scheduler.ErrContextIsNil)
	}
	if got := task.State(); got != scheduler.StateScheduled {
		t.Fatalf("State() = %v, want %v", got, scheduler.StateScheduled)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("the task func ran %d times, want 0", n)
	}
}

func TestScheduledTaskRunStoresTheResult(t *testing.T) {
	adapter, calls := countingFunc("t", func(*astral.Context) error { return errTaskFailed })
	task := NewScheduledTask(adapter)

	if err := task.Run(astral.NewContext(nil)); !errors.Is(err, errTaskFailed) {
		t.Fatalf("Run() = %v, want %v", err, errTaskFailed)
	}
	waitDone(t, task)

	if got := task.State(); got != scheduler.StateDone {
		t.Fatalf("State() = %v, want %v", got, scheduler.StateDone)
	}
	if err := task.Err(); !errors.Is(err, errTaskFailed) {
		t.Fatalf("Err() = %v, want %v", err, errTaskFailed)
	}

	if err := task.Run(astral.NewContext(nil)); !errors.Is(err, scheduler.ErrInvalidState) {
		t.Fatalf("second Run() = %v, want %v", err, scheduler.ErrInvalidState)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("the task func ran %d times, want 1", n)
	}
}

func TestScheduledTaskCancelBeforeRun(t *testing.T) {
	adapter, calls := countingFunc("t", func(*astral.Context) error { return nil })
	task := NewScheduledTask(adapter)

	task.CancelWithError(errTaskFailed)
	waitDone(t, task)

	if got := task.State(); got != scheduler.StateDone {
		t.Fatalf("State() = %v, want %v", got, scheduler.StateDone)
	}
	if err := task.Err(); !errors.Is(err, errTaskFailed) {
		t.Fatalf("Err() = %v, want %v", err, errTaskFailed)
	}

	if err := task.Run(astral.NewContext(nil)); !errors.Is(err, scheduler.ErrInvalidState) {
		t.Fatalf("Run() after cancel = %v, want %v", err, scheduler.ErrInvalidState)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("the task func ran %d times, want 0", n)
	}
}

func TestScheduledTaskCancelAfterDoneKeepsTheError(t *testing.T) {
	task := NewScheduledTask(scheduler.Func("t", func(*astral.Context) error { return errTaskFailed }))

	_ = task.Run(astral.NewContext(nil))
	waitDone(t, task)

	task.Cancel()

	if err := task.Err(); !errors.Is(err, errTaskFailed) {
		t.Fatalf("Err() after Cancel = %v, want %v", err, errTaskFailed)
	}
	if got := task.State(); got != scheduler.StateDone {
		t.Fatalf("State() after Cancel = %v, want %v", got, scheduler.StateDone)
	}
}

func TestScheduledTaskCancelWhileRunningCancelsItsContext(t *testing.T) {
	started := make(chan struct{})
	task := NewScheduledTask(scheduler.Func("t", func(ctx *astral.Context) error {
		close(started)
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-time.After(2 * time.Second):
			return errors.New("the task was never canceled")
		}
	}))

	result := make(chan error, 1)
	go func() { result <- task.Run(astral.NewContext(nil)) }()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the task func did not start within 2s")
	}

	task.CancelWithError(errTaskFailed)

	select {
	case err := <-result:
		if !errors.Is(err, errTaskFailed) {
			t.Fatalf("Run() = %v, want %v", err, errTaskFailed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return within 2s of the cancel")
	}

	// note: State is unsynchronized, so it is read only after Done closes.
	waitDone(t, task)
	if got := task.State(); got != scheduler.StateDone {
		t.Fatalf("State() = %v, want %v", got, scheduler.StateDone)
	}
}
