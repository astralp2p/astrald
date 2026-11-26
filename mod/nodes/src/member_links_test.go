package nodes

import (
	"errors"
	"net"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/scheduler"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// memberUser is a user module whose member set the test scripts; a set
// error makes every read fail.
type memberUser struct {
	usermod.Module
	ready chan struct{}

	mu      sync.Mutex
	members []*astral.Identity
	err     error
}

func (u *memberUser) set(members ...*astral.Identity) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.members = members
}

func (u *memberUser) fail(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.err = err
}

func (u *memberUser) Ready() <-chan struct{} { return u.ready }

func (u *memberUser) ReadLocalSwarm() ([]*astral.Identity, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.err != nil {
		return nil, u.err
	}
	return slices.Clone(u.members), nil
}

func (u *memberUser) LocalSwarm() []*astral.Identity {
	list, _ := u.ReadLocalSwarm()
	return list
}

func (u *memberUser) IsSwarmMember(id *astral.Identity) bool {
	return slices.ContainsFunc(u.LocalSwarm(), id.IsEqual)
}

// cancelTask is a scheduled task that records its cancel.
type cancelTask struct {
	scheduler.ScheduledTask
	cancelled atomic.Bool
}

func (t *cancelTask) Cancel() { t.cancelled.Store(true) }

// linkTaskScheduler records the member link task scheduled for each target
// and runs none.
type linkTaskScheduler struct {
	scheduler.Module

	mu    sync.Mutex
	tasks map[string][]*cancelTask
}

func (s *linkTaskScheduler) Schedule(task scheduler.Task, _ ...scheduler.Done) (scheduler.ScheduledTask, error) {
	link, ok := task.(*memberLinkTask)
	if !ok {
		return nil, errors.New("linkTaskScheduler runs member link tasks only")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := &cancelTask{}
	s.tasks[link.target.String()] = append(s.tasks[link.target.String()], t)
	return t, nil
}

// running returns the targets whose last scheduled task is not cancelled, sorted.
func (s *linkTaskScheduler) running() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []string
	for target, tasks := range s.tasks {
		if !tasks[len(tasks)-1].cancelled.Load() {
			list = append(list, target)
		}
	}
	sort.Strings(list)
	return list
}

// scheduled returns how many link tasks were scheduled for id.
func (s *linkTaskScheduler) scheduled(id *astral.Identity) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tasks[id.String()])
}

// memberLinksFixture is a nodes module on nodeID whose swarm members user
// scripts, whose scheduler records the member link tasks, and whose link
// closes are recorded on closed, each with whether that member's task was
// already cancelled.
type memberLinksFixture struct {
	mod    *Module
	nodeID *astral.Identity
	user   *memberUser
	sched  *linkTaskScheduler
	closed chan closedLinks
}

type closedLinks struct {
	id        *astral.Identity
	cancelled bool
}

func newMemberLinksFixture(t *testing.T, members ...*astral.Identity) *memberLinksFixture {
	t.Helper()
	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	f := &memberLinksFixture{
		nodeID: astral.GenerateIdentity(),
		user:   &memberUser{ready: make(chan struct{})},
		sched:  &linkTaskScheduler{tasks: map[string][]*cancelTask{}},
		closed: make(chan closedLinks, 16),
	}
	close(f.user.ready)
	f.user.set(append(members, f.nodeID)...)
	f.mod = &Module{
		Deps: Deps{User: f.user, Scheduler: f.sched, Events: &recordingEvents{}},
		node: &identityNode{id: f.nodeID},
		log:  log.New(f.nodeID),
		ctx:  ctx,
	}
	f.mod.linkPool = NewLinkPool(f.mod)
	f.mod.memberLinks.close = func(id *astral.Identity) error {
		f.sched.mu.Lock()
		tasks := f.sched.tasks[id.String()]
		f.sched.mu.Unlock()
		f.closed <- closedLinks{id: id, cancelled: len(tasks) > 0 && tasks[len(tasks)-1].cancelled.Load()}
		return nil
	}

	f.mod.runMemberLinks(ctx)
	return f
}

// receive delivers a local event carrying data.
func (f *memberLinksFixture) receive(t *testing.T, data astral.Object) {
	t.Helper()
	event := &events.Event{ID: astral.NewNonce(), SourceID: f.nodeID, Data: data}
	if err := f.mod.ReceiveObject(&recordingDrop{sender: f.nodeID, object: event}); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}
}

// requireRunning waits until the running link tasks are exactly those of ids,
// and fails if that changes within absenceWindow.
func (f *memberLinksFixture) requireRunning(t *testing.T, ids ...*astral.Identity) {
	t.Helper()
	var want []string
	for _, id := range ids {
		want = append(want, id.String())
	}
	sort.Strings(want)
	eventually(t, "link tasks of exactly the latest members", func() bool { return slices.Equal(f.sched.running(), want) })
	time.Sleep(absenceWindow)
	if got := f.sched.running(); !slices.Equal(got, want) {
		t.Fatalf("running link tasks %v; want %v", got, want)
	}
}

// requireClosed waits for the links of id to close after its task was cancelled.
func (f *memberLinksFixture) requireClosed(t *testing.T, id *astral.Identity) {
	t.Helper()
	select {
	case c := <-f.closed:
		if !c.id.IsEqual(id) {
			t.Fatalf("closed the links of %v; want %v", c.id, id)
		}
		if !c.cancelled {
			t.Fatalf("closed the links of %v before its link task was cancelled", id)
		}
	case <-time.After(effectTimeout):
		t.Fatalf("the links of %v were not closed", id)
	}
}

// requireNoClose fails if any member's links close within absenceWindow.
func (f *memberLinksFixture) requireNoClose(t *testing.T) {
	t.Helper()
	select {
	case c := <-f.closed:
		t.Fatalf("closed the links of %v; want no close", c.id)
	case <-time.After(absenceWindow):
	}
}

// TestMemberLinksStartWithTheFirstMemberSet: once the user module is ready,
// every member but this node gets a link task.
func TestMemberLinksStartWithTheFirstMemberSet(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	f := newMemberLinksFixture(t, a, b)
	f.requireRunning(t, a, b)
	if n := f.sched.scheduled(f.nodeID); n != 0 {
		t.Fatalf("scheduled %v link tasks to this node; want none", n)
	}
}

// TestSwarmMemberJoinedMaintainsALink: a SwarmMemberJoined for a member
// that entered the set starts one link task for it, and a repeated event
// starts no second one.
func TestSwarmMemberJoinedMaintainsALink(t *testing.T) {
	a := astral.GenerateIdentity()
	f := newMemberLinksFixture(t)
	f.requireRunning(t)

	f.user.set(f.nodeID, a)
	f.receive(t, &user.SwarmMemberJoined{NodeID: a})
	f.requireRunning(t, a)

	f.receive(t, &user.SwarmMemberJoined{NodeID: a})
	f.requireRunning(t, a)
	if n := f.sched.scheduled(a); n != 1 {
		t.Fatalf("scheduled %v link tasks for the member; want 1", n)
	}
	f.requireNoClose(t)
}

// TestSwarmMemberLeftClosesTheMembersLinks: a SwarmMemberLeft for a member
// that left the set stops its link task and then closes its links; the other
// members' tasks and links stay.
func TestSwarmMemberLeftClosesTheMembersLinks(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	f := newMemberLinksFixture(t, a, b)
	f.requireRunning(t, a, b)

	f.user.set(f.nodeID, a)
	f.receive(t, &user.SwarmMemberLeft{NodeID: b})
	f.requireClosed(t, b)
	f.requireRunning(t, a)
	f.requireNoClose(t)
}

// TestMemberEventsEndInTheLatestSet: the link tasks follow the member set
// read at each event, not the node an event names, so events that arrive
// out of order, or name another node, still end in the latest set.
func TestMemberEventsEndInTheLatestSet(t *testing.T) {
	a, b, c := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	f := newMemberLinksFixture(t, a)
	f.requireRunning(t, a)

	// b joined and left again; the Left arrives before the Joined
	f.receive(t, &user.SwarmMemberLeft{NodeID: b})
	f.receive(t, &user.SwarmMemberJoined{NodeID: b})
	f.requireRunning(t, a)
	if n := f.sched.scheduled(b); n != 0 {
		t.Fatalf("scheduled %v link tasks for a member no longer in the set; want 0", n)
	}

	// c joined and a left; only a Left naming c arrives
	f.user.set(f.nodeID, c)
	f.receive(t, &user.SwarmMemberLeft{NodeID: c})
	f.requireClosed(t, a)
	f.requireRunning(t, c)

	// a SwarmMemberJoined from another node is no signal here
	f.user.set(f.nodeID, c, b)
	event := &events.Event{ID: astral.NewNonce(), SourceID: b, Data: &user.SwarmMemberJoined{NodeID: b}}
	if err := f.mod.ReceiveObject(&recordingDrop{sender: b, object: event}); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}
	f.requireRunning(t, c)
	f.requireNoClose(t)
}

// TestMemberLinksKeepTasksWhenTheSwarmReadFails: a failed read of the member
// set stops no link task and closes no link.
func TestMemberLinksKeepTasksWhenTheSwarmReadFails(t *testing.T) {
	a := astral.GenerateIdentity()
	f := newMemberLinksFixture(t, a)
	f.requireRunning(t, a)

	f.user.fail(errors.New("read failed"))
	f.receive(t, &user.SwarmMemberLeft{NodeID: a})
	f.requireRunning(t, a)
	f.requireNoClose(t)
}

// TestMemberLinksWaitForTheUserModule: a member event before the user module
// is ready starts no link task.
func TestMemberLinksWaitForTheUserModule(t *testing.T) {
	a := astral.GenerateIdentity()
	nodeID := astral.GenerateIdentity()
	u := &memberUser{ready: make(chan struct{})}
	u.set(nodeID, a)
	sched := &linkTaskScheduler{tasks: map[string][]*cancelTask{}}
	mod := &Module{Deps: Deps{User: u, Scheduler: sched}, node: &identityNode{id: nodeID}, log: log.New(nodeID)}

	mod.reconcileMemberLinks()
	if n := sched.scheduled(a); n != 0 {
		t.Fatalf("scheduled %v link tasks before the user module was ready; want 0", n)
	}
}

// TestPeerSeenStartsAMissingLinkTask: a member seen nearby that has no link
// task gets one; a node that is no member gets none.
func TestPeerSeenStartsAMissingLinkTask(t *testing.T) {
	a, stranger := astral.GenerateIdentity(), astral.GenerateIdentity()
	f := newMemberLinksFixture(t)

	f.user.set(f.nodeID, a)
	seen := func(id *astral.Identity) *nearby.PeerSeen {
		return &nearby.PeerSeen{NodeID: id, Via: nearby.ViaHint, SourceIP: ip.IP(net.IPv4(192, 168, 1, 7))}
	}
	f.receive(t, seen(stranger))
	f.receive(t, seen(a))
	f.requireRunning(t, a)
	if n := f.sched.scheduled(stranger); n != 0 {
		t.Fatalf("scheduled %v link tasks for a stranger; want 0", n)
	}
}

// TestPeerSeenWakesAnUnlinkedMember: a member seen nearby whose task waits
// out its backoff dials at once.
func TestPeerSeenWakesAnUnlinkedMember(t *testing.T) {
	f := newLinkTaskFixture(failEvery, false)
	f.mod.Deps.User = &memberUser{members: []*astral.Identity{f.target}}
	f.mod.linkPool = NewLinkPool(f.mod)
	done, cancel := f.run(t)

	f.requireAttempt(t, 1, effectTimeout)
	f.requireBackoff(t)
	f.mod.onMemberSeen(f.target)
	f.requireAttempt(t, 2, wakeDialBound)

	cancel()
	requireCanceled(t, done)
}
