package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// entryChan is a log.EntryLogger that forwards every entry to a channel.
type entryChan chan *log.Entry

func (c entryChan) LogEntry(e *log.Entry) { c <- e }

// TestScheduleReturnsNilErrorForFailingTask covers Schedule: the error a task returns
// is only logged, and never leaks into Schedule's own result. Run with -race.
func TestScheduleReturnsNilErrorForFailingTask(t *testing.T) {
	const n = 200

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	entries := make(entryChan, n)
	l := &log.Logger{}
	l.AddLogger(entries)

	mod := &Module{log: l, ctx: astral.NewContext(ctx)}

	taskErr := errors.New("task failed")
	task := scheduler.Func("failing", func(*astral.Context) error { return taskErr })

	for i := 0; i < n; i++ {
		sTask, err := mod.Schedule(task)
		if err != nil {
			t.Fatalf("Schedule returned %v; want nil", err)
		}
		<-sTask.Done()

		// the runner logs the task error after it stores it, so wait for the log
		<-entries
	}
}
