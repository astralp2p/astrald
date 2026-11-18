package user

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// wakeDialBound is how soon a woken link task must dial (the parent task's criterion 7).
const wakeDialBound = time.Second

// errDialFailed is the outcome of a failed link attempt.
var errDialFailed = errors.New("dial failed")

// linkNodes is a nodes module that reports the identities set linked as linked
// and hands out ensure-link tasks for dialScheduler to finish.
type linkNodes struct {
	nodesmod.Module

	mu     sync.Mutex
	linked map[string]bool
}

func newLinkNodes() *linkNodes {
	return &linkNodes{linked: make(map[string]bool)}
}

func (n *linkNodes) setLinked(id *astral.Identity, linked bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.linked[id.String()] = linked
}

func (n *linkNodes) IsLinked(id *astral.Identity) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.linked[id.String()]
}

func (n *linkNodes) NewEnsureLinkTask(target *astral.Identity, _, _ []string, _ bool) nodesmod.EnsureLinkTask {
	return &ensureLinkStub{target: target}
}

// ensureLinkStub is the ensure-link task dialScheduler finishes.
type ensureLinkStub struct {
	nodesmod.EnsureLinkTask
	target *astral.Identity
}

// dialScheduler finishes every scheduled task at once with the error outcome
// returns for that attempt, and reports each attempt number on attempts.
type dialScheduler struct {
	scheduler.Module
	attempts chan int
	outcome  func(attempt int) error

	mu    sync.Mutex
	count int
}

func newDialScheduler(outcome func(attempt int) error) *dialScheduler {
	return &dialScheduler{attempts: make(chan int, 64), outcome: outcome}
}

func (s *dialScheduler) Schedule(task scheduler.Task, _ ...scheduler.Done) (scheduler.ScheduledTask, error) {
	if _, ok := task.(*ensureLinkStub); !ok {
		return nil, errors.New("dialScheduler runs ensure-link tasks only")
	}

	n := s.nextAttempt()
	s.attempts <- n
	return finishedTask{err: s.outcome(n)}, nil
}

// nextAttempt counts one more attempt and returns its number.
func (s *dialScheduler) nextAttempt() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	return s.count
}

// finishedTask is a scheduled task that is done with err.
type finishedTask struct {
	scheduler.ScheduledTask
	err error
}

func (t finishedTask) Done() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

func (t finishedTask) Err() error { return t.err }

// backoffClock records each backoff delay on delays. A wait it starts ends at
// once when fire is set, and never otherwise.
type backoffClock struct {
	delays chan time.Duration
	fire   bool
}

func (c *backoffClock) after(d time.Duration) <-chan time.Time {
	c.delays <- d
	ch := make(chan time.Time, 1)
	if c.fire {
		ch <- time.Time{}
	}
	return ch
}

// linkTaskFixture is a MaintainLinkTask for target on a module with fake nodes,
// a fake scheduler and a fake backoff clock.
type linkTaskFixture struct {
	mod       *Module
	target    *astral.Identity
	task      *MaintainLinkTask
	nodes     *linkNodes
	scheduler *dialScheduler
	clock     *backoffClock
}

func newLinkTaskFixture(outcome func(int) error, fire bool) *linkTaskFixture {
	f := &linkTaskFixture{
		target:    astral.GenerateIdentity(),
		nodes:     newLinkNodes(),
		scheduler: newDialScheduler(outcome),
		clock:     &backoffClock{delays: make(chan time.Duration, 64), fire: fire},
	}
	nodeID := astral.GenerateIdentity()
	f.mod = &Module{
		Deps: Deps{Nodes: f.nodes, Scheduler: f.scheduler},
		node: &identityNode{id: nodeID},
		log:  log.New(nodeID),
	}
	f.task = f.mod.NewMaintainLinkTask(f.target).(*MaintainLinkTask)
	f.task.after = f.clock.after
	return f
}

// run starts the task and returns its result channel and the cancel of its context.
func (f *linkTaskFixture) run(t *testing.T) (<-chan error, context.CancelFunc) {
	t.Helper()

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() { done <- f.task.Run(ctx) }()
	return done, cancel
}

// requireAttempt waits up to bound for link attempt n.
func (f *linkTaskFixture) requireAttempt(t *testing.T, n int, bound time.Duration) {
	t.Helper()
	select {
	case got := <-f.scheduler.attempts:
		if got != n {
			t.Fatalf("link attempt %v; want attempt %v", got, n)
		}
	case <-time.After(bound):
		t.Fatalf("link attempt %v did not come within %v", n, bound)
	}
}

// requireBackoff waits for the task to start a backoff wait and returns its delay.
func (f *linkTaskFixture) requireBackoff(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-f.clock.delays:
		return d
	case <-time.After(effectTimeout):
		t.Fatal("the task started no backoff wait")
		return 0
	}
}

func failEvery(int) error { return errDialFailed }

// TestMaintainLinkTaskWakeDuringBackoff: a Wake during a 15-minute-class backoff
// wait makes the task dial at once; the fake clock never ends the wait.
func TestMaintainLinkTaskWakeDuringBackoff(t *testing.T) {
	f := newLinkTaskFixture(failEvery, false)
	done, cancel := f.run(t)

	f.requireAttempt(t, 1, effectTimeout)
	if d := f.requireBackoff(t); d != linkRetryMin {
		t.Fatalf("first backoff %v; want %v", d, linkRetryMin)
	}

	f.task.Wake()
	f.requireAttempt(t, 2, wakeDialBound)
	if d := f.requireBackoff(t); d != 2*linkRetryMin {
		t.Fatalf("second backoff %v; want %v", d, 2*linkRetryMin)
	}

	cancel()
	requireCanceled(t, done)
}

// TestMaintainLinkTaskCancelDuringBackoff: cancelling the task's context during a
// backoff wait ends the task with the context's error.
func TestMaintainLinkTaskCancelDuringBackoff(t *testing.T) {
	f := newLinkTaskFixture(failEvery, false)
	done, cancel := f.run(t)

	f.requireAttempt(t, 1, effectTimeout)
	f.requireBackoff(t)

	cancel()
	requireCanceled(t, done)
}

func requireCanceled(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v; want %v", err, context.Canceled)
		}
	case <-time.After(effectTimeout):
		t.Fatal("Run did not return after the cancel")
	}
}

// TestMaintainLinkTaskBackoffDoublesAndResets: the delay doubles from 1 s after
// every failure up to 15 min, and a success starts it over. A LinkClosedEvent
// for the target's last link wakes the idle task.
func TestMaintainLinkTaskBackoffDoublesAndResets(t *testing.T) {
	const failures = 12
	// note: the attempt after the reset backoff succeeds, so the loop idles before the cancel.
	f := newLinkTaskFixture(func(n int) error {
		if n == failures+1 || n == failures+3 {
			return nil
		}
		return errDialFailed
	}, true)
	done, cancel := f.run(t)

	want := []time.Duration{}
	for d := linkRetryMin; len(want) < failures; d = min(2*d, linkRetryMax) {
		want = append(want, d)
	}
	var got []time.Duration
	for n := 1; n <= failures; n++ {
		f.requireAttempt(t, n, effectTimeout)
		got = append(got, f.requireBackoff(t))
	}
	if !slices.Equal(got, want) {
		t.Fatalf("backoff delays %v; want %v", got, want)
	}
	if got[failures-1] != linkRetryMax {
		t.Fatalf("last delay %v; want the cap %v", got[failures-1], linkRetryMax)
	}

	f.requireAttempt(t, failures+1, effectTimeout)

	f.task.ReceiveEvent(&events.Event{Data: &nodes.LinkClosedEvent{RemoteIdentity: f.target, LinkCount: 0}})
	f.requireAttempt(t, failures+2, wakeDialBound)
	if d := f.requireBackoff(t); d != linkRetryMin {
		t.Fatalf("backoff after a success %v; want %v", d, linkRetryMin)
	}
	f.requireAttempt(t, failures+3, effectTimeout)

	cancel()
	requireCanceled(t, done)
}

// TestMaintainLinkTaskActsOnAWakeDuringAnAttempt: a LinkClosedEvent or a Wake
// that comes while an attempt runs, which then succeeds, makes the task dial
// once more.
func TestMaintainLinkTaskActsOnAWakeDuringAnAttempt(t *testing.T) {
	closed := func(f *linkTaskFixture) {
		f.task.ReceiveEvent(&events.Event{Data: &nodes.LinkClosedEvent{RemoteIdentity: f.target, LinkCount: 0}})
	}
	woken := func(f *linkTaskFixture) { f.task.Wake() }

	for name, during := range map[string]func(*linkTaskFixture){"link closed": closed, "wake": woken} {
		t.Run(name, func(t *testing.T) {
			var f *linkTaskFixture
			f = newLinkTaskFixture(func(n int) error {
				if n == 1 {
					during(f)
				}
				return nil
			}, false)
			done, cancel := f.run(t)

			f.requireAttempt(t, 1, effectTimeout)
			f.requireAttempt(t, 2, wakeDialBound)

			cancel()
			requireCanceled(t, done)
		})
	}
}

// TestMaintainLinkTaskIgnoresWakesOfALinkedTarget: a Wake while the target is
// linked, a LinkClosedEvent of another node and a partial disconnect leave the
// idle task idle.
func TestMaintainLinkTaskIgnoresWakesOfALinkedTarget(t *testing.T) {
	f := newLinkTaskFixture(func(int) error { return nil }, false)
	f.nodes.setLinked(f.target, true)

	f.task.Wake()
	f.task.ReceiveEvent(&events.Event{Data: &nodes.LinkClosedEvent{RemoteIdentity: astral.GenerateIdentity(), LinkCount: 0}})
	f.task.ReceiveEvent(&events.Event{Data: &nodes.LinkClosedEvent{RemoteIdentity: f.target, LinkCount: 1}})

	if f.task.actionRequired.Load() {
		t.Fatal("the task needs action; want it idle")
	}
	select {
	case <-f.task.wake:
		t.Fatal("the task was signalled; want no signal")
	default:
	}
}

// TestMaintainLinkTaskDoesNotDialAfterCancel: a task whose context is already
// cancelled when the link needs work returns without dialing, so a link close
// racing the cancel of an expelled sibling's task cannot redial it.
func TestMaintainLinkTaskDoesNotDialAfterCancel(t *testing.T) {
	f := newLinkTaskFixture(failEvery, false)
	f.task.Wake()

	ctx, cancel := astral.NewContext(nil).WithCancel()
	cancel()

	if err := f.task.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v; want %v", err, context.Canceled)
	}
	if n := f.scheduler.nextAttempt() - 1; n != 0 {
		t.Fatalf("the cancelled task dialed %v times; want 0", n)
	}
}

// addrAdded is an address change that adds addr.
func addrAdded(addr string) *events.Event {
	return &events.Event{Data: &ip.EventNetworkAddressChanged{Added: []ip.IP{ip.IP(net.ParseIP(addr))}}}
}

// TestMaintainLinkTaskWakesOnAddressChange: an ip.EventNetworkAddressChanged
// that adds a routable address during a backoff wait makes the unlinked task dial at once and restarts the
// backoff from linkRetryMin; the fake clock never ends the wait.
func TestMaintainLinkTaskWakesOnAddressChange(t *testing.T) {
	f := newLinkTaskFixture(failEvery, false)
	done, cancel := f.run(t)

	f.requireAttempt(t, 1, effectTimeout)
	f.requireBackoff(t)
	f.task.Wake()
	f.requireAttempt(t, 2, wakeDialBound)
	if d := f.requireBackoff(t); d != 2*linkRetryMin {
		t.Fatalf("second backoff %v; want %v", d, 2*linkRetryMin)
	}

	f.task.ReceiveEvent(addrAdded("192.168.1.10"))
	f.requireAttempt(t, 3, wakeDialBound)
	if d := f.requireBackoff(t); d != linkRetryMin {
		t.Fatalf("backoff after an address change %v; want %v", d, linkRetryMin)
	}

	cancel()
	requireCanceled(t, done)
}

// TestMaintainLinkTaskIgnoresAddressChangeWhileLinked: an address change while
// the target is linked leaves the idle task idle.
func TestMaintainLinkTaskIgnoresAddressChangeWhileLinked(t *testing.T) {
	f := newLinkTaskFixture(func(int) error { return nil }, false)
	f.nodes.setLinked(f.target, true)

	f.task.ReceiveEvent(addrAdded("192.168.1.10"))

	f.requireIdle(t)
}

// TestMaintainLinkTaskIgnoresAddressChangesThatAddNoRoutableAddress: removing
// an address or adding only a link-local one neither wakes the unlinked task
// nor restarts its backoff.
func TestMaintainLinkTaskIgnoresAddressChangesThatAddNoRoutableAddress(t *testing.T) {
	f := newLinkTaskFixture(failEvery, false)

	f.task.ReceiveEvent(&events.Event{Data: &ip.EventNetworkAddressChanged{
		Removed: []ip.IP{ip.IP(net.ParseIP("192.168.1.10"))},
	}})
	f.task.ReceiveEvent(addrAdded("fe80::1"))

	f.requireIdle(t)
}

// requireIdle fails if the task needs action, restarts its backoff, or was signalled.
func (f *linkTaskFixture) requireIdle(t *testing.T) {
	t.Helper()
	if f.task.actionRequired.Load() || f.task.resetBackoff.Load() {
		t.Fatal("the task needs action; want it idle")
	}
	select {
	case <-f.task.wake:
		t.Fatal("the task was signalled; want no signal")
	default:
	}
}

// logLines records the text of every log entry.
type logLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *logLines) LogEntry(e *log.Entry) {
	var b strings.Builder
	for _, obj := range e.Objects {
		fmt.Fprint(&b, obj)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, b.String())
}

// count returns how many lines contain text.
func (l *logLines) count(text string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, line := range l.lines {
		if strings.Contains(line, text) {
			n++
		}
	}
	return n
}

// TestMaintainLinkTaskLogsBrokenOnlyForAClosedLink: a Wake during the first,
// successful dial makes the task dial once more without logging the standing
// link as broken; a link that then closes is logged as broken and redialed.
func TestMaintainLinkTaskLogsBrokenOnlyForAClosedLink(t *testing.T) {
	var f *linkTaskFixture
	f = newLinkTaskFixture(func(n int) error {
		if n == 1 {
			f.task.Wake()
		}
		f.nodes.setLinked(f.target, true)
		return nil
	}, false)
	lines := &logLines{}
	f.mod.log.AddLogger(lines)
	done, cancel := f.run(t)

	f.requireAttempt(t, 1, effectTimeout)
	f.requireAttempt(t, 2, wakeDialBound)
	if n := lines.count("established"); n != 1 {
		t.Fatalf("%v \"established\" lines; want 1", n)
	}
	if n := lines.count("is broken"); n != 0 {
		t.Fatalf("%v \"is broken\" lines while the link stands; want 0", n)
	}

	f.nodes.setLinked(f.target, false)
	f.task.ReceiveEvent(&events.Event{Data: &nodes.LinkClosedEvent{RemoteIdentity: f.target, LinkCount: 0}})
	f.requireAttempt(t, 3, wakeDialBound)
	if n := lines.count("is broken, trying to reconnect"); n != 1 {
		t.Fatalf("%v \"is broken, trying to reconnect\" lines after the close; want 1", n)
	}

	cancel()
	requireCanceled(t, done)
}
