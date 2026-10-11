package scheduler

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// pollErr calls Err until the task is done and returns a channel that closes when polling stops.
func pollErr(sTask *ScheduledTask) <-chan struct{} {
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			_ = sTask.Err()
			select {
			case <-sTask.Done():
				return
			default:
				runtime.Gosched()
			}
		}
	}()
	return stopped
}

// TestScheduledTaskErrDuringCancel covers ScheduledTask.Err: reading the error while
// CancelWithError stores it is safe. Run with -race.
func TestScheduledTaskErrDuringCancel(t *testing.T) {
	errX := errors.New("canceled by test")
	sTask := NewScheduledTask(scheduler.Func("blocking", func(ctx *astral.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}))

	go sTask.Run(astral.NewContext(context.Background()))
	for sTask.State() != scheduler.StateRunning {
		runtime.Gosched()
	}

	stopped := pollErr(sTask)
	sTask.CancelWithError(errX)
	waitDone(t, sTask)
	<-stopped

	if err := sTask.Err(); !errors.Is(err, errX) {
		t.Fatalf("Err() = %v; want %v", err, errX)
	}
}

// TestScheduledTaskErrDuringCompletion covers ScheduledTask.Err: reading the error while
// Run stores the task's own result is safe. Run with -race.
func TestScheduledTaskErrDuringCompletion(t *testing.T) {
	release := make(chan struct{})
	sTask := NewScheduledTask(scheduler.Func("blocking", func(*astral.Context) error {
		<-release
		return errTaskFailed
	}))

	go sTask.Run(astral.NewContext(context.Background()))
	for sTask.State() != scheduler.StateRunning {
		runtime.Gosched()
	}

	stopped := pollErr(sTask)
	close(release)
	waitDone(t, sTask)
	<-stopped

	if err := sTask.Err(); !errors.Is(err, errTaskFailed) {
		t.Fatalf("Err() = %v; want %v", err, errTaskFailed)
	}
}
