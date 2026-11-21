package nodes

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	ipmod "github.com/astralp2p/astrald/mod/ip"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// scheduledRun is one task the fake scheduler received.
type scheduledRun struct {
	task scheduler.Task
	// delay is the time left on the first dependency when the task was scheduled.
	delay time.Duration
}

// fakeScheduler records scheduled tasks and, when auto is set, runs each one
// once its dependencies are done.
type fakeScheduler struct {
	scheduler.Module
	ctx  *astral.Context
	auto bool

	mu   sync.Mutex
	runs []scheduledRun
}

func (s *fakeScheduler) Ready() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (s *fakeScheduler) Schedule(task scheduler.Task, deps ...scheduler.Done) (scheduler.ScheduledTask, error) {
	run := scheduledRun{task: task}
	if len(deps) > 0 {
		// note: Module.Run and the task pass a *astral.Context that ends after the delay.
		if deadline, ok := deps[0].(*astral.Context).Deadline(); ok {
			run.delay = time.Until(deadline)
		}
	}

	s.mu.Lock()
	s.runs = append(s.runs, run)
	s.mu.Unlock()

	if s.auto {
		go func() {
			for _, d := range deps {
				<-d.Done()
			}
			if s.ctx.Err() == nil {
				_ = task.Run(s.ctx)
			}
		}()
	}
	return nil, nil
}

// scheduledTasks returns the scheduled tasks of type T in order.
func scheduledTasks[T scheduler.Task](s *fakeScheduler) (out []scheduledRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.runs {
		if _, ok := r.task.(T); ok {
			out = append(out, r)
		}
	}
	return
}

// refreshFixture is an exchangeFixture with a fake scheduler and a sibling refresh period.
type refreshFixture struct {
	*exchangeFixture
	sched *fakeScheduler
}

func newRefreshFixture(t *testing.T, interval time.Duration) *refreshFixture {
	t.Helper()

	f := &refreshFixture{exchangeFixture: newExchangeFixture(t)}
	f.sched = &fakeScheduler{ctx: f.mod.ctx}
	f.mod.Deps.Scheduler = f.sched
	f.mod.siblingRefreshInterval = interval
	f.mod.localRefreshInterval = defaultLocalRefreshInterval
	return f
}

// run runs one refresh task.
func (f *refreshFixture) run(t *testing.T) {
	t.Helper()
	if err := f.mod.newRefreshSiblingEndpointsTask().Run(f.mod.ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// runModule runs Module.Run until the test ends. The fixture's links are
// placeholders without a channel, so they leave the pool before Run closes it.
func (f *refreshFixture) runModule(t *testing.T) {
	t.Helper()

	ctx, cancel := astral.NewContext(nil).WithCancel()
	f.sched.ctx = ctx
	done := make(chan error, 1)
	go func() { done <- f.mod.Run(ctx) }()
	t.Cleanup(func() {
		_ = f.mod.linkPool.links.Clear()
		cancel()
		<-done
	})
}

// TestRefreshSiblingTask_PullsLinkedSiblingOnce (criterion 1): one run pulls
// the endpoints of a linked sibling from that sibling exactly once. The pull
// goes through resolveRemote, the only path out of the module in the fixture.
func TestRefreshSiblingTask_PullsLinkedSiblingOnce(t *testing.T) {
	f := newRefreshFixture(t, defaultSiblingRefreshInterval)
	s := f.member(true)

	f.run(t)

	f.requirePull(t, s, s)
	f.requireNoPull(t)
}

// TestRefreshSiblingTask_RenewsRowExpiry (criterion 2): a stored tcp row that
// expires in about 1 day expires in about 7 days after the run.
func TestRefreshSiblingTask_RenewsRowExpiry(t *testing.T) {
	f := newRefreshFixture(t, defaultSiblingRefreshInterval)
	s := f.member(true)
	row := tcpEndpoint("198.51.100.9")

	oneDay := time.Now().Add(24 * time.Hour).UTC()
	if err := f.mod.db.AddEndpoint(s, row.Network(), row.Address(), &oneDay); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	expiry := func() time.Duration {
		rows, err := f.mod.db.FindEndpoints(s)
		if err != nil || len(rows) != 1 || rows[0].ExpiresAt == nil {
			t.Fatalf("rows = %v, %v; want one row with an expiry", rows, err)
		}
		return time.Until(*rows[0].ExpiresAt)
	}
	if d := expiry(); d < 23*time.Hour || d > 25*time.Hour {
		t.Fatalf("seeded expiry in %v; want about 1 day", d)
	}

	f.answer = []*nodes.EndpointWithTTL{nodes.NewEndpointWithTTL(row, 7*24*time.Hour)}
	f.run(t)
	f.requirePull(t, s, s)

	eventually(t, "row renewal", func() bool { return expiry() > 6*24*time.Hour })
	if d := expiry(); d > 7*24*time.Hour+time.Minute {
		t.Fatalf("renewed expiry in %v; want at most 7 days", d)
	}
}

// TestRefreshSiblingTask_SkipsUnlinkedSelfAndNonSwarm (criterion 3): an
// unlinked sibling, this node and a linked node outside the swarm are not
// pulled; the linked sibling is.
func TestRefreshSiblingTask_SkipsUnlinkedSelfAndNonSwarm(t *testing.T) {
	f := newRefreshFixture(t, defaultSiblingRefreshInterval)
	linked := f.member(true)
	f.member(false)
	f.stranger()
	// note: newExchangeFixture puts this node in its own swarm.
	f.mod.linkPool.links.Add(&Link{id: astral.NewNonce(), conn: &identityConn{remote: f.self}})

	f.run(t)

	f.requirePull(t, linked, linked)
	f.requireNoPull(t)
}

// pullProbe replaces the fixture's resolveRemote with a function that records
// each call, its context deadline and the most pulls that overlapped.
type pullProbe struct {
	calls    atomic.Int32
	active   atomic.Int32
	overlap  atomic.Int32
	deadline chan time.Duration
	// gate blocks the first call until it is closed.
	gate chan struct{}
}

func newPullProbe(f *refreshFixture) *pullProbe {
	p := &pullProbe{deadline: make(chan time.Duration, 16), gate: make(chan struct{})}
	f.mod.resolveRemote = func(ctx *astral.Context, _, _ *astral.Identity) ([]*nodes.EndpointWithTTL, error) {
		n := p.calls.Add(1)
		if d, ok := ctx.Deadline(); ok {
			p.deadline <- time.Until(d)
		} else {
			p.deadline <- -1
		}

		if now := p.active.Add(1); now > p.overlap.Load() {
			p.overlap.Store(now)
		}
		defer p.active.Add(-1)

		if n == 1 {
			<-p.gate
		}
		return nil, nil
	}
	return p
}

// TestRefreshSiblingTask_SharesSyncGateWithPartialClose (criterion 4): a task
// pull and a partial-close pull for one sibling started together run once plus
// one rerun, never in parallel, and both are bounded by endpointsPullTimeout.
func TestRefreshSiblingTask_SharesSyncGateWithPartialClose(t *testing.T) {
	f := newRefreshFixture(t, defaultSiblingRefreshInterval)
	s := f.member(true)
	probe := newPullProbe(f)

	f.run(t)
	eventually(t, "first pull", func() bool { return probe.calls.Load() == 1 })

	// note: the first pull blocks; the close request folds into one rerun.
	f.mod.onPartialLinkClose(&nodes.LinkClosedEvent{RemoteIdentity: s, LinkCount: 1})
	f.mod.onPartialLinkClose(&nodes.LinkClosedEvent{RemoteIdentity: s, LinkCount: 1})
	time.Sleep(absenceWindow)
	if n := probe.calls.Load(); n != 1 {
		t.Fatalf("%v pulls started while the first runs; want 1", n)
	}

	close(probe.gate)
	eventually(t, "rerun", func() bool { return probe.calls.Load() == 2 })
	time.Sleep(absenceWindow)
	if n := probe.calls.Load(); n != 2 {
		t.Fatalf("%v pulls in total; want 2 (one plus one rerun)", n)
	}
	if n := probe.overlap.Load(); n != 1 {
		t.Fatalf("%v pulls overlapped; want never in parallel", n)
	}

	for range 2 {
		if d := <-probe.deadline; d <= 0 || d > endpointsPullTimeout {
			t.Fatalf("pull deadline in %v; want within (0, %v]", d, endpointsPullTimeout)
		}
	}
}

// TestJitter_Bounds (criterion 5): jitter(24h, 0.10) stays within 21.6 h to
// 26.4 h over 1000 calls and returns more than one value.
func TestJitter_Bounds(t *testing.T) {
	const d = 24 * time.Hour
	seen := map[time.Duration]bool{}
	for range 1000 {
		got := jitter(d, 0.10)
		if got < 21*time.Hour+36*time.Minute || got > 26*time.Hour+24*time.Minute {
			t.Fatalf("jitter(24h, 0.10) = %v; want within 21.6h to 26.4h", got)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Fatal("jitter returned one value in 1000 calls")
	}
}

// TestRefreshSiblingTask_ReschedulesOnce (criterion 5, as YTVsOej6bp changed
// it): a run schedules exactly one next task with a delay of the 35 min local
// refresh period plus or minus 10%.
func TestRefreshSiblingTask_ReschedulesOnce(t *testing.T) {
	f := newRefreshFixture(t, defaultSiblingRefreshInterval)
	f.member(true)

	f.run(t)

	next := scheduledTasks[*RefreshSiblingEndpointsTask](f.sched)
	if len(next) != 1 {
		t.Fatalf("%v tasks scheduled; want 1", len(next))
	}
	// note: the delay is read a moment after it was set, so it is a little short.
	if d := next[0].delay; d < 31*time.Minute || d > 38*time.Minute+30*time.Second {
		t.Fatalf("next run in %v; want 35 min plus or minus 10%%", d)
	}
}

// TestRefreshSiblingTask_PullTimesOutAndReschedules (criterion 6): a sibling
// that never answers holds only its own pull, which carries a deadline of
// endpointsPullTimeout; the run returns and the next task is scheduled.
// note: the test does not wait out the 20 s; it checks the deadline the pull
// is given and that a hung pull does not hold the run.
func TestRefreshSiblingTask_PullTimesOutAndReschedules(t *testing.T) {
	f := newRefreshFixture(t, defaultSiblingRefreshInterval)
	f.member(true)

	started := make(chan time.Duration, 1)
	f.mod.resolveRemote = func(ctx *astral.Context, _, _ *astral.Identity) ([]*nodes.EndpointWithTTL, error) {
		d, _ := ctx.Deadline()
		started <- time.Until(d)
		<-ctx.Done()
		return nil, ctx.Err()
	}

	done := make(chan struct{})
	go func() {
		f.run(t)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(effectTimeout):
		t.Fatal("Run waited on a pull that never answers")
	}

	select {
	case d := <-started:
		if d <= 0 || d > endpointsPullTimeout {
			t.Fatalf("pull deadline in %v; want within (0, %v]", d, endpointsPullTimeout)
		}
	case <-time.After(effectTimeout):
		t.Fatal("no pull started")
	}
	if n := len(scheduledTasks[*RefreshSiblingEndpointsTask](f.sched)); n != 1 {
		t.Fatalf("%v tasks scheduled while the pull hangs; want 1", n)
	}
}

// TestRefreshSiblingTask_RequestsAnnounceCheck: each run requests the EP3
// announcement check, which pushes once for a new set and nothing for an
// unchanged one.
func TestRefreshSiblingTask_RequestsAnnounceCheck(t *testing.T) {
	f := newRefreshFixture(t, defaultSiblingRefreshInterval)
	f.member(true)
	f.own.set(endpointsWithTTL("198.51.100.1")...)

	f.run(t)
	eventually(t, "first announcement", func() bool { return f.announced() == 1 })

	f.run(t)
	waitOut()
	if n := f.announced(); n != 1 {
		t.Fatalf("%v announcements after an unchanged run; want 1", n)
	}

	f.own.set(endpointsWithTTL("198.51.100.1", "198.51.100.2")...)
	f.run(t)
	eventually(t, "announcement of the changed set", func() bool { return f.announced() == 2 })
}

// TestRefreshSiblingTask_RunSchedulesFirstAfterInterval: Module.Run schedules
// the task once with a delay of about the local refresh period, not at once,
// and the first run pulls no sibling without a local row.
func TestRefreshSiblingTask_RunSchedulesFirstAfterInterval(t *testing.T) {
	f := newRefreshFixture(t, defaultSiblingRefreshInterval)
	f.member(true)

	f.runModule(t)

	eventually(t, "scheduling", func() bool {
		return len(scheduledTasks[*RefreshSiblingEndpointsTask](f.sched)) > 0
	})
	first := scheduledTasks[*RefreshSiblingEndpointsTask](f.sched)
	if len(first) != 1 {
		t.Fatalf("%v refresh tasks scheduled; want 1", len(first))
	}
	if d := first[0].delay; d < 31*time.Minute || d > 38*time.Minute+30*time.Second {
		t.Fatalf("first run in %v; want about 35 min", d)
	}
	f.requireNoPull(t)

	if err := first[0].task.Run(f.mod.ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	f.requireNoPull(t)
}

// TestRefreshSiblingTask_RepeatsOnShortInterval (criterion 7): with a short
// period the task runs at least twice in a row and pulls the sibling each time.
func TestRefreshSiblingTask_RepeatsOnShortInterval(t *testing.T) {
	f := newRefreshFixture(t, 50*time.Millisecond)
	f.mod.localRefreshInterval = 50 * time.Millisecond
	f.sched.auto = true
	s := f.member(true)

	f.runModule(t)

	f.requirePull(t, s, s)
	f.requirePull(t, s, s)
}

// advance moves the clock forward by d.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// pullsOf collects n pulls and returns the identities pulled, by string form.
func (f *refreshFixture) pullsOf(t *testing.T, n int) map[string]bool {
	t.Helper()
	got := map[string]bool{}
	for range n {
		select {
		case p := <-f.pulls:
			if !p.resolver.IsEqual(p.identity) {
				t.Fatalf("pulled endpoints of %v from %v; want first-hand", p.identity, p.resolver)
			}
			got[p.identity.String()] = true
		case <-time.After(effectTimeout):
			t.Fatalf("%v of %v pulls started", len(got), n)
		}
	}
	return got
}

// TestRefreshSiblingTask_KeepsLocalRowsAliveWhileLinked (YTVsOej6bp criterion
// 2): on a fake clock, a linked sibling with a local row is re-pulled on every
// run, each run well inside LocalEndpointTTL of the last, and a sibling with
// only public rows waits for the 24 h run. After the link closes no re-pull
// happens and the local row expires.
func TestRefreshSiblingTask_KeepsLocalRowsAliveWhileLinked(t *testing.T) {
	f := newRefreshFixture(t, defaultSiblingRefreshInterval)
	clock := &fakeClock{now: time.Now()}
	f.mod.refreshClock = clock
	local, public := f.member(true), f.member(true)
	localRow, publicRow := tcpEndpoint("192.168.1.20"), tcpEndpoint("198.51.100.9")

	f.mod.resolveRemote = func(_ *astral.Context, resolver, identity *astral.Identity) ([]*nodes.EndpointWithTTL, error) {
		f.pulls <- pull{resolver: resolver, identity: identity}
		if identity.IsEqual(local) {
			return []*nodes.EndpointWithTTL{nodes.NewEndpointWithTTL(localRow, ipmod.LocalEndpointTTL)}, nil
		}
		return []*nodes.EndpointWithTTL{nodes.NewEndpointWithTTL(publicRow, ipmod.PublicEndpointTTL)}, nil
	}
	soon := time.Now().Add(ipmod.LocalEndpointTTL).UTC()
	week := time.Now().Add(ipmod.PublicEndpointTTL).UTC()
	if err := f.mod.db.AddEndpoint(local, localRow.Network(), localRow.Address(), &soon); err != nil {
		t.Fatalf("seed local row: %v", err)
	}
	if err := f.mod.db.AddEndpoint(public, publicRow.Network(), publicRow.Address(), &week); err != nil {
		t.Fatalf("seed public row: %v", err)
	}
	// note: as Module.Run does, so the first full run is a day away.
	f.mod.deferFullRefresh(clock.Now())

	var elapsed time.Duration
	for elapsed < 2*ipmod.LocalEndpointTTL {
		f.run(t)
		f.requirePull(t, local, local)
		f.requireNoPull(t)

		runs := scheduledTasks[*RefreshSiblingEndpointsTask](f.sched)
		next := runs[len(runs)-1].delay
		if next <= 0 || next >= ipmod.LocalEndpointTTL/2 {
			t.Fatalf("next run in %v; want well inside %v", next, ipmod.LocalEndpointTTL)
		}
		clock.advance(next)
		elapsed += next
	}

	clock.advance(defaultSiblingRefreshInterval)
	f.run(t)
	if got := f.pullsOf(t, 2); !got[local.String()] || !got[public.String()] {
		t.Fatalf("the 24 h run pulled %v; want both linked siblings", got)
	}

	// note: the pull stores its answer after it is counted.
	eventually(t, "renewal by the 24 h run", func() bool {
		return time.Until(expiryOf(t, f.mod.db, local, localRow.Network(), localRow.Address())) > ipmod.LocalEndpointTTL-time.Minute
	})
	_ = f.mod.linkPool.links.Clear()
	lapse := time.Now().Add(300 * time.Millisecond).UTC()
	if err := f.mod.db.AddEndpoint(local, localRow.Network(), localRow.Address(), &lapse); err != nil {
		t.Fatalf("seed lapsing row: %v", err)
	}
	clock.advance(30 * time.Minute)
	f.run(t)
	f.requireNoPull(t)
	eventually(t, "local row expiry", func() bool {
		return expiryOf(t, f.mod.db, local, localRow.Network(), localRow.Address()).IsZero()
	})
}
