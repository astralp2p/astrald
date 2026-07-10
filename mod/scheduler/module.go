package scheduler

import (
	"fmt"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/events"
)

const ModuleName = "scheduler"

// Module defines the public interface of the task scheduler.
type Module interface {
	Ready() <-chan struct{}
	// Schedule schedules a task for execution once all dependencies are done
	Schedule(task Task, deps ...Done) (ScheduledTask, error)
}

// Task represents a unit of work to be executed by the scheduler.
type Task interface {
	fmt.Stringer
	Run(*astral.Context) error
}

// Done is an interface used for waiting for the task to finish.
type Done interface {
	Done() <-chan struct{}
}

// Releaser is an interface used for releasing lockable dependencies
type Releaser interface {
	Release()
}

// EventReceiver is an interface used to propagate events to running tasks
type EventReceiver interface {
	ReceiveEvent(e *events.Event)
}
