package user

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// linkTaskScheduler records the scheduled link task of each target and runs none.
type linkTaskScheduler struct {
	scheduler.Module

	mu    sync.Mutex
	tasks map[string]*cancelTask
}

func (s *linkTaskScheduler) Schedule(task scheduler.Task, _ ...scheduler.Done) (scheduler.ScheduledTask, error) {
	link, ok := task.(*MaintainLinkTask)
	if !ok {
		return nil, errors.New("linkTaskScheduler runs link tasks only")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	t := &cancelTask{}
	s.tasks[link.Target.String()] = t
	return t, nil
}

// task returns the link task scheduled for id, or nil.
func (s *linkTaskScheduler) task(id *astral.Identity) *cancelTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tasks[id.String()]
}

// cancelTask is a scheduled task that records its cancel.
type cancelTask struct {
	scheduler.ScheduledTask
	cancelled atomic.Bool
}

func (t *cancelTask) Cancel() { t.cancelled.Store(true) }

// linkerEnv is a module whose swarm is the subjects of e.query.found.
type linkerEnv struct {
	*memberEnv
	sched *linkTaskScheduler
}

func newLinkerEnv(t *testing.T, members ...*astral.Identity) *linkerEnv {
	e := &linkerEnv{memberEnv: newMemberEnv(t), sched: &linkTaskScheduler{tasks: map[string]*cancelTask{}}}
	nodeID := astral.GenerateIdentity()
	e.mod.node = &identityNode{id: nodeID}
	e.mod.log = log.New(nodeID)
	e.mod.Scheduler = e.sched
	e.setSwarm(t, append(members, nodeID)...)
	return e
}

func (e *linkerEnv) setSwarm(t *testing.T, members ...*astral.Identity) {
	e.query.found = nil
	for _, m := range members {
		e.query.found = append(e.query.found, memberContract(t, e.issuer, m))
	}
}

// requireLinked fails unless id has a registered, running link task.
func (e *linkerEnv) requireLinked(t *testing.T, id *astral.Identity) {
	t.Helper()
	if _, ok := e.mod.sibs.Get(id.String()); !ok {
		t.Fatalf("%v has no registered link task", id)
	}
	if task := e.sched.task(id); task == nil || task.cancelled.Load() {
		t.Fatalf("the link task of %v is missing or stopped", id)
	}
}

// requireStopped fails unless the link task of id is cancelled and unregistered.
func (e *linkerEnv) requireStopped(t *testing.T, id *astral.Identity) {
	t.Helper()
	if _, ok := e.mod.sibs.Get(id.String()); ok {
		t.Fatalf("%v still has a registered link task", id)
	}
	if task := e.sched.task(id); task == nil || !task.cancelled.Load() {
		t.Fatalf("the link task of %v was not cancelled", id)
	}
}

// TestSiblingLinkerStopsTasksOfFormerMembers: a linker run after a member left
// the swarm stops that member's link task and keeps the others' tasks.
func TestSiblingLinkerStopsTasksOfFormerMembers(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	e := newLinkerEnv(t, a, b)

	e.mod.runSiblingLinker()
	e.requireLinked(t, a)
	e.requireLinked(t, b)
	kept := e.sched.task(a)

	e.setSwarm(t, a, e.mod.node.Identity())
	e.mod.runSiblingLinker()

	e.requireStopped(t, b)
	e.requireLinked(t, a)
	if e.sched.task(a) != kept {
		t.Fatal("the linker replaced the link task of a current member")
	}
}

// TestClearedContractStopsEverySiblingTask: clearing the active contract stops
// the link task of every sibling.
func TestClearedContractStopsEverySiblingTask(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	e := newLinkerEnv(t, a, b)
	e.mod.runSiblingLinker()

	e.mod.onActiveContractChanged(nil)

	e.requireStopped(t, a)
	e.requireStopped(t, b)
}

// TestSiblingLinkerKeepsTasksWhenTheSwarmQueryFails: a failed contract query
// stops no link task.
func TestSiblingLinkerKeepsTasksWhenTheSwarmQueryFails(t *testing.T) {
	a := astral.GenerateIdentity()
	e := newLinkerEnv(t, a)
	e.mod.runSiblingLinker()

	e.query.err = errors.New("query failed")
	e.mod.runSiblingLinker()

	e.requireLinked(t, a)
}
