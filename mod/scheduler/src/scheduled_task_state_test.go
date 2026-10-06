package scheduler

import (
	"context"
	"runtime"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// TestScheduledTaskStateDuringRun covers ScheduledTask.State: reading the state while
// Run changes it is safe. Run with -race.
func TestScheduledTaskStateDuringRun(t *testing.T) {
	release := make(chan struct{})
	sTask := NewScheduledTask(scheduler.Func("blocking", func(*astral.Context) error {
		<-release
		return nil
	}))

	go sTask.Run(astral.NewContext(context.Background()))

	// poll the state until the task is running
	for sTask.State() != scheduler.StateRunning {
		runtime.Gosched()
	}
	close(release)

	<-sTask.Done()
	if s := sTask.State(); s != scheduler.StateDone {
		t.Fatalf("state after Done is %v; want %v", s, scheduler.StateDone)
	}
}
