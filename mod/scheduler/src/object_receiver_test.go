package scheduler

import (
	"sync/atomic"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/events"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// identityNode is an astral.Node that only answers Identity.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

// eventTask is a task that counts the events it receives.
type eventTask struct {
	received atomic.Int32
}

func (*eventTask) String() string               { return "event_task" }
func (*eventTask) Run(*astral.Context) error    { return nil }
func (t *eventTask) ReceiveEvent(*events.Event) { t.received.Add(1) }

// runningTask is a scheduled task that reports itself running.
type runningTask struct {
	scheduler.ScheduledTask
	task scheduler.Task
}

func (t *runningTask) State() scheduler.State { return scheduler.StateRunning }
func (t *runningTask) Task() scheduler.Task   { return t.task }

// acceptCounter is an objects.Drop that counts its Accept calls.
type acceptCounter struct {
	sender  *astral.Identity
	object  astral.Object
	accepts atomic.Int32
}

func (d *acceptCounter) SenderID() *astral.Identity { return d.sender }
func (d *acceptCounter) Object() astral.Object      { return d.object }
func (d *acceptCounter) Accept(bool) error {
	d.accepts.Add(1)
	return nil
}

// TestReceiveEventRequiresLocalSender covers the scheduler receiver: a running task
// receives this node's own event, and never an event sent by another node.
func TestReceiveEventRequiresLocalSender(t *testing.T) {
	cases := []struct {
		name     string
		local    bool
		received int32
	}{
		{"from another node", false, 0},
		{"from this node", true, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodeID := astral.GenerateIdentity()
			task := &eventTask{}

			mod := &Module{node: &identityNode{id: nodeID}}
			mod.queue.Add(&runningTask{task: task})

			sender := astral.GenerateIdentity()
			if tc.local {
				sender = nodeID
			}

			event := &events.Event{ID: astral.NewNonce(), SourceID: nodeID, Data: &astral.Nil{}}
			drop := &acceptCounter{sender: sender, object: event}

			if err := mod.ReceiveObject(drop); err != nil {
				t.Fatalf("ReceiveObject: %v", err)
			}

			if n := task.received.Load(); n != tc.received {
				t.Fatalf("the running task received %d events; want %d", n, tc.received)
			}

			if n := drop.accepts.Load(); n != 0 {
				t.Fatalf("accepted the event %d times; want none", n)
			}
		})
	}
}
