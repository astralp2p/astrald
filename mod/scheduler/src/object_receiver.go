package scheduler

import (
	"github.com/astralp2p/astrald/mod/events"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// ReceiveObject fans out incoming event objects to all currently running tasks that implement EventReceiver.
func (mod *Module) ReceiveObject(drop objects.Drop) (err error) {
	switch o := drop.Object().(type) {
	case *events.Event:
		for _, task := range mod.queue.Clone() {
			// skip non-running tasks
			if task.State() != scheduler.StateRunning {
				continue
			}

			// propagate the event to the task if supported
			if receiver, ok := task.Task().(scheduler.EventReceiver); ok {
				receiver.ReceiveEvent(o)
			}
		}
	}

	return nil
}
