package scheduler

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// TestScheduledTaskCancelWithErrorWhileRunning covers CancelWithError: canceling a running
// task with an error makes Err report that error, not the task's own return value.
func TestScheduledTaskCancelWithErrorWhileRunning(t *testing.T) {
	errX := errors.New("canceled by test")

	sTask := NewScheduledTask(scheduler.Func("blocking", func(ctx *astral.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}))

	go sTask.Run(astral.NewContext(context.Background()))

	// wait until the task is running
	for sTask.State() != scheduler.StateRunning {
		runtime.Gosched()
	}

	sTask.CancelWithError(errX)
	<-sTask.Done()

	if err := sTask.Err(); !errors.Is(err, errX) {
		t.Fatalf("Err() = %v; want %v", err, errX)
	}
}
