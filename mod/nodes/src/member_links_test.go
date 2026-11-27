package nodes

import (
	"errors"
	"io"
	"maps"
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
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/scheduler"
)

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
	link, ok := task.(*MaintainLinkTask)
	if !ok {
		return nil, errors.New("linkTaskScheduler runs member link tasks only")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := &cancelTask{}
	s.tasks[link.Target.String()] = append(s.tasks[link.Target.String()], t)
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
// scripts, whose scheduler records the member link tasks, and whose pool holds
// a real link to every member and every node a test links.
type memberLinksFixture struct {
	mod    *Module
	nodeID *astral.Identity
	user   *swarmUser
	sched  *linkTaskScheduler
	// pulls receives the resolver and the identity of every endpoints pull.
	pulls chan pull

	mu    sync.Mutex
	links map[*Link]*astral.Identity
	// checked holds the links requireClosed saw close.
	checked map[*Link]bool
}

func newMemberLinksFixture(t *testing.T, members ...*astral.Identity) *memberLinksFixture {
	t.Helper()
	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	f := &memberLinksFixture{
		nodeID:  astral.GenerateIdentity(),
		user:    &swarmUser{ready: make(chan struct{})},
		sched:   &linkTaskScheduler{tasks: map[string][]*cancelTask{}},
		pulls:   make(chan pull, 16),
		links:   map[*Link]*astral.Identity{},
		checked: map[*Link]bool{},
	}
	close(f.user.ready)
	f.user.set(append(members, f.nodeID)...)
	f.mod = &Module{
		Deps: Deps{User: f.user, Scheduler: f.sched, Events: &recordingEvents{}, Objects: &pushObjects{}},
		node: &identityNode{id: f.nodeID},
		log:  log.New(f.nodeID),
		ctx:  ctx,
	}
	f.mod.linkPool = NewLinkPool(f.mod)
	// why: the pull of a new member's endpoints ends in a failed pull; no test reads a row it stores.
	f.mod.resolveRemote = func(_ *astral.Context, resolver, identity *astral.Identity) ([]*nodes.EndpointWithTTL, error) {
		f.pulls <- pull{resolver: resolver, identity: identity}
		return nil, errors.New("no resolver in the test")
	}
	for _, m := range members {
		f.link(t, m)
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

// requireClosed waits for the links of exactly ids to close, and for no other
// link to close within absenceWindow. A link must close after the link task of
// its node was cancelled.
func (f *memberLinksFixture) requireClosed(t *testing.T, ids ...*astral.Identity) {
	t.Helper()
	for _, id := range ids {
		for link, remote := range f.snapshot() {
			if !remote.IsEqual(id) || f.checked[link] {
				continue
			}
			select {
			case <-link.Done():
			case <-time.After(effectTimeout):
				t.Fatalf("the links of %v were not closed", id)
			}
			f.checked[link] = true
			f.sched.mu.Lock()
			tasks := f.sched.tasks[id.String()]
			stopped := len(tasks) == 0 || tasks[len(tasks)-1].cancelled.Load()
			f.sched.mu.Unlock()
			if !stopped {
				t.Fatalf("closed the links of %v before its link task was cancelled", id)
			}
		}
	}
}

// link adds a link to id to the pool, which closes like a real link, and
// returns it.
func (f *memberLinksFixture) link(t *testing.T, id *astral.Identity) *Link {
	t.Helper()
	link, far := newPeerLink(t, &Module{}, id, time.Minute, time.Minute)
	// why: the link's close notice is written to the pipe and waits for a reader.
	go io.Copy(io.Discard, far)
	t.Cleanup(func() { link.Close() })
	f.mod.linkPool.links.Add(link)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.links[link] = id
	return link
}

func (f *memberLinksFixture) snapshot() map[*Link]*astral.Identity {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.links)
}

// requireNoClose fails if a link that no requireClosed saw close is closed
// within absenceWindow.
func (f *memberLinksFixture) requireNoClose(t *testing.T) {
	t.Helper()
	time.Sleep(absenceWindow)
	for link, id := range f.snapshot() {
		select {
		case <-link.Done():
			if !f.checked[link] {
				t.Fatalf("closed a link of %v; want no close", id)
			}
		default:
		}
	}
}

// requirePulled waits for the endpoints of exactly ids to be pulled from
// themselves, each once, and fails on any other pull within absenceWindow.
func (f *memberLinksFixture) requirePulled(t *testing.T, ids ...*astral.Identity) {
	t.Helper()
	want := map[string]bool{}
	for _, id := range ids {
		want[id.String()] = true
	}
	deadline := time.After(effectTimeout)
	for len(want) > 0 {
		select {
		case p := <-f.pulls:
			if !want[p.identity.String()] || !p.resolver.IsEqual(p.identity) {
				t.Fatalf("pulled the endpoints of %v from %v; want of %v from themselves, each once", p.identity, p.resolver, ids)
			}
			delete(want, p.identity.String())
		case <-deadline:
			t.Fatalf("the endpoints of %v were not pulled", ids)
		}
	}
	select {
	case p := <-f.pulls:
		t.Fatalf("pulled the endpoints of %v; want only %v, each once", p.identity, ids)
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

// TestNewMemberLinkRefreshesTheMembersEndpoints: the reconcile that starts a
// member's link task pulls that member's endpoints from the member, once, and
// never this node's own; a reconcile that starts no task pulls nothing.
//
// why: a membership reaches a node only through SwarmState sync, which
// carries no endpoints (Decisions 2026-11-24), so the new member's task has
// none to dial until the pull.
func TestNewMemberLinkRefreshesTheMembersEndpoints(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	f := newMemberLinksFixture(t, a)
	f.requirePulled(t, a)

	f.user.set(f.nodeID, a, b)
	f.receive(t, &user.SwarmMemberJoined{NodeID: b})
	f.requirePulled(t, b)

	f.receive(t, &user.SwarmMemberJoined{NodeID: b})
	f.requirePulled(t)
}

// TestEmptiedMemberSetStopsEveryMemberLink: a member set that reads empty,
// as after the active contract is cleared or when this node holds its own
// ban, stops the link task of every member and closes their links.
func TestEmptiedMemberSetStopsEveryMemberLink(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	f := newMemberLinksFixture(t, a, b)
	f.requireRunning(t, a, b)

	f.user.set()
	f.receive(t, &user.SwarmMemberLeft{NodeID: f.nodeID})
	f.requireClosed(t, a, b)
	f.requireRunning(t)
	f.requireNoClose(t)
}

// TestReconcileKeepsAnUnansweredBannedNodesLinksOpen: a member event re-reads
// the member set, and the links of a linked node the user banned stay open,
// though it is no member here: they carry its ban to it until it answers. The
// links of an unbanned non-member stay too, and so does the link task of a
// member.
func TestReconcileKeepsAnUnansweredBannedNodesLinksOpen(t *testing.T) {
	a, banned, stranger := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	f := newMemberLinksFixture(t, a)
	f.requireRunning(t, a)
	f.link(t, banned)
	f.link(t, stranger)

	f.user.ban(banned)
	f.receive(t, &user.SwarmMemberLeft{NodeID: banned})
	f.receive(t, &user.SwarmMemberJoined{NodeID: a})
	f.requireRunning(t, a)
	f.requireNoClose(t)
	if n := f.sched.scheduled(banned); n != 0 {
		t.Fatalf("scheduled %v link tasks for the banned node; want 0", n)
	}
}

// TestBannedMemberLeavingStopsItsTaskAndKeepsItsLinks: a banned member leaves
// the member set; its link task stops, so nothing redials it, and its links
// stay open until it answers its ban.
func TestBannedMemberLeavingStopsItsTaskAndKeepsItsLinks(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	f := newMemberLinksFixture(t, a, b)
	f.requireRunning(t, a, b)

	f.user.ban(b)
	f.user.set(f.nodeID, a)
	f.receive(t, &user.SwarmMemberLeft{NodeID: b})
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

// TestMemberLinksWaitForTheUserModule: a member event before the user module
// is ready starts no link task.
func TestMemberLinksWaitForTheUserModule(t *testing.T) {
	a := astral.GenerateIdentity()
	nodeID := astral.GenerateIdentity()
	u := &swarmUser{ready: make(chan struct{})}
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
	f.mod.Deps.User = &swarmUser{swarm: []*astral.Identity{f.target}}
	f.mod.linkPool = NewLinkPool(f.mod)
	done, cancel := f.run(t)

	f.requireAttempt(t, 1, effectTimeout)
	f.requireBackoff(t)
	f.mod.onMemberSeen(f.target)
	f.requireAttempt(t, 2, wakeDialBound)

	cancel()
	requireCanceled(t, done)
}
