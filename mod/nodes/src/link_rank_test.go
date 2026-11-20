package nodes

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	exonetapi "github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// netEndpoint is an endpoint that answers only its network.
type netEndpoint struct {
	exonetapi.Endpoint
	network string
}

func (e netEndpoint) Network() string { return e.network }

// rankedConn is a conn over network between local and remote; outbound marks the local end as the dialer.
type rankedConn struct {
	astral.Conn
	local, remote *astral.Identity
	outbound      bool
	network       string
}

func (c *rankedConn) LocalIdentity() *astral.Identity    { return c.local }
func (c *rankedConn) RemoteIdentity() *astral.Identity   { return c.remote }
func (c *rankedConn) Outbound() bool                     { return c.outbound }
func (c *rankedConn) LocalEndpoint() exonetapi.Endpoint  { return nil }
func (c *rankedConn) RemoteEndpoint() exonetapi.Endpoint { return netEndpoint{network: c.network} }

// newRankedPool returns a module whose node identity is lower than the returned peer's, with a context and a log.
func newRankedPool(t *testing.T) (*Module, *astral.Identity) {
	t.Helper()
	local, remote := astral.GenerateIdentity(), astral.GenerateIdentity()
	if string(identityKey(local)) > string(identityKey(remote)) {
		local, remote = remote, local
	}
	ctx, cancel := astral.NewContext(context.Background()).WithCancel()
	t.Cleanup(cancel)
	mod := &Module{node: &identityNode{id: local}, log: log.New(local), ctx: ctx}
	mod.linkPool = NewLinkPool(mod)
	return mod, remote
}

// rankedLink pools a link to remote over network that no loop drives; byLocal marks it dialed by the module's node.
// note: in newRankedPool the module's node is the lower identity, so byLocal is dialedByLower.
func rankedLink(t *testing.T, mod *Module, remote *astral.Identity, network string, byLocal bool, opts ...selectLinkOpt) *Link {
	t.Helper()
	setup := func(l *Link) {
		l.conn = &rankedConn{local: mod.node.Identity(), remote: remote, outbound: byLocal, network: network}
		l.outbound = byLocal
		// why: CloseWithError writes its close frame on the link's channel.
		l.Channel = channel.New(&recordingLink{}, channel.WithLockedWrites())
	}
	return newSelectable(t, mod, remote, append([]selectLinkOpt{setup}, opts...)...)
}

// countingStrategy is a strategy that counts its joins and runs join, when set, before it finishes.
type countingStrategy struct {
	joins int
	mu    sync.Mutex
	join  func()
}

func (s *countingStrategy) Name() string { return "counting" }

func (s *countingStrategy) Join(*astral.Context) (<-chan struct{}, func()) {
	s.count()
	if s.join != nil {
		s.join()
	}
	done := make(chan struct{})
	close(done)
	return done, func() {}
}

func (s *countingStrategy) Build(*astral.Identity) nodesmod.LinkStrategy { return s }

func (s *countingStrategy) count() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.joins++
}

func (s *countingStrategy) joined() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.joins
}

// TestNonForcedRetrieveAndSelectPreferKcpOverTor: U3 AC1. A tor link dialed by the lower identity and a
// newer kcp link dialed by the higher identity: RetrieveLink without ForceNew and SelectLinkWith return the
// kcp link, and RetrieveLink joins no strategy.
func TestNonForcedRetrieveAndSelectPreferKcpOverTor(t *testing.T) {
	mod, remote := newRankedPool(t)
	stub := &countingStrategy{}
	mod.strategyFactories.Set("counting", stub)
	tor := rankedLink(t, mod, remote, "tor", true, aged(time.Minute))
	kcp := rankedLink(t, mod, remote, "kcp", false, aged(time.Second))

	if got := mod.linkPool.findSurvivor(remote, nil); got != tor {
		t.Fatalf("premise: findSurvivor returned %v; want the tor link %v the lower identity dialed", got.id, tor.id)
	}
	r := awaitResult(t, "non-forced", mod.linkPool.RetrieveLink(astral.NewContext(context.Background()), remote, WithStrategies("counting")))
	if r.Err != nil || r.Link != kcp {
		t.Fatalf("RetrieveLink answered (link %v, err %v); want the kcp link %v", r.Link != nil, r.Err, kcp.id)
	}
	if n, linkers := stub.joined(), mod.linkPool.linkers.Len(); n != 0 || linkers != 0 {
		t.Fatalf("RetrieveLink joined %v strategy rounds and made %v linkers; want none", n, linkers)
	}
	if got := mod.linkPool.SelectLinkWith(remote); got != kcp {
		t.Fatalf("SelectLinkWith returned %v; want the kcp link %v", got.id, kcp.id)
	}
}

// TestRetrieveFallbackPrefersHigherRank: the links a round leaves in the pool without handing them to the
// caller resolve to the higher-ranked one when the strategies finish.
func TestRetrieveFallbackPrefersHigherRank(t *testing.T) {
	mod, remote := newRankedPool(t)
	var kcp *Link
	stub := &countingStrategy{join: func() {
		rankedLink(t, mod, remote, "tor", true, aged(time.Minute))
		kcp = rankedLink(t, mod, remote, "kcp", false, aged(time.Second))
	}}
	mod.strategyFactories.Set("counting", stub)

	r := awaitResult(t, "fallback", mod.linkPool.RetrieveLink(astral.NewContext(context.Background()), remote, WithStrategies("counting")))
	if stub.joined() != 1 {
		t.Fatalf("RetrieveLink joined %v rounds; want 1 with no link pooled", stub.joined())
	}
	if r.Err != nil || r.Link != kcp {
		t.Fatalf("RetrieveLink answered (link %v, err %v); want the kcp link", r.Link != nil, r.Err)
	}
}

// TestBestLinkEqualRankMatchesSurvivor: U3 AC2. Of links of one rank, bestLink returns what findSurvivor returns, in every pool order.
func TestBestLinkEqualRankMatchesSurvivor(t *testing.T) {
	type spec struct {
		byLocal bool
		age     time.Duration
	}
	for _, network := range []string{"kcp", "tor", "unknown"} {
		for _, links := range [][]spec{
			{{true, time.Minute}, {false, time.Second}},
			{{false, time.Minute}, {true, time.Second}},
			{{true, time.Minute}, {true, time.Second}},
			{{false, time.Minute}, {false, time.Second}},
			{{false, time.Second}, {true, time.Hour}, {false, time.Minute}, {true, time.Second}},
		} {
			for _, reverse := range []bool{false, true} {
				mod, remote := newRankedPool(t)
				for i := range links {
					s := links[i]
					if reverse {
						s = links[len(links)-1-i]
					}
					rankedLink(t, mod, remote, network, s.byLocal, aged(s.age))
				}
				want := mod.linkPool.findSurvivor(remote, nil)
				if got := mod.linkPool.bestLink(remote, nil); got != want {
					t.Fatalf("%v %v reverse %v: bestLink returned %v; want findSurvivor's %v", network, links, reverse, got.id, want.id)
				}
			}
		}
	}
}

// TestSelectLowerRankOnlyUnderPressure: U3 AC3. SelectLinkWith returns a lower-ranked link only when every
// higher-ranked link is under high pressure, and the best-ranked link when every link is.
func TestSelectLowerRankOnlyUnderPressure(t *testing.T) {
	type spec struct {
		network string
		high    bool
	}
	for _, c := range []struct {
		name  string
		links []spec
		want  string
	}{
		{"all calm", []spec{{"tor", false}, {"kcp", false}, {"tcp", false}}, "tcp"},
		{"top under pressure", []spec{{"tor", false}, {"kcp", false}, {"tcp", true}}, "kcp"},
		{"all but the lowest under pressure", []spec{{"tor", false}, {"kcp", true}, {"tcp", true}}, "tor"},
		{"lower calm, top calm", []spec{{"tor", true}, {"tcp", false}}, "tcp"},
		{"all under pressure", []spec{{"tor", true}, {"kcp", true}, {"tcp", true}}, "tcp"},
	} {
		t.Run(c.name, func(t *testing.T) {
			mod, remote := newRankedPool(t)
			for i, s := range c.links {
				// why ages: the lower-ranked links are the newer, so age alone never explains the pick.
				rankedLink(t, mod, remote, s.network, true, aged(time.Duration(len(c.links)-i)*time.Minute), pressured(s.high))
			}
			if got := mod.linkPool.SelectLinkWith(remote); got == nil || got.Network() != c.want {
				t.Fatalf("SelectLinkWith returned %v; want the %v link", got, c.want)
			}
		})
	}
}

// TestSelectRankBeforeSuspect: DL3 AC7. A suspect tcp link beats a healthy tor link: the rank comes before the suspect state.
func TestSelectRankBeforeSuspect(t *testing.T) {
	for _, c := range []struct {
		name    string
		suspect selectLinkOpt
	}{
		{"ping in flight", pinging()},
		{"probe due", idleExpired()},
	} {
		t.Run(c.name, func(t *testing.T) {
			mod, remote := newRankedPool(t)
			tcp := rankedLink(t, mod, remote, "tcp", true, aged(time.Minute), c.suspect)
			tor := rankedLink(t, mod, remote, "tor", true, aged(time.Second))
			if !tcp.Suspect() || tor.Suspect() {
				t.Fatalf("premise: tcp suspect %v, tor suspect %v; want true, false", tcp.Suspect(), tor.Suspect())
			}
			if got := mod.linkPool.SelectLinkWith(remote); got != tcp {
				t.Fatalf("SelectLinkWith returned the %v link; want the suspect tcp link", got.Network())
			}
		})
	}
}

// migration is one call of the module's migrate seam.
type migration struct {
	old       astral.Nonce
	target    *Link
	oldClosed bool // the old link had an error when the call began
}

// recordMigrations makes mod record its migrations instead of running them.
func recordMigrations(mod *Module) chan migration {
	calls := make(chan migration, 16)
	mod.migrate = func(old astral.Nonce, target *Link) {
		l := mod.getLinkByID(old)
		calls <- migration{old: old, target: target, oldClosed: l == nil || l.Err() != nil}
	}
	return calls
}

// openSession opens a session on link; migratable ages it past minSessionAge.
func openSession(t *testing.T, link *Link, migratable bool) *session {
	t.Helper()
	sess, ok := link.mux.createSession(astral.NewNonce(), link.RemoteIdentity(), link.RemoteIdentity(), "test", true, defaultBufferSize)
	if !ok {
		t.Fatal("create session")
	}
	sess.setState(stateOpen)
	if migratable {
		sess.createdAt = time.Now().Add(-2 * minSessionAge)
	}
	return sess
}

// pendingTimers reports how many timers wait on c.
func (c *fakeClock) pendingTimers() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

// requireMigratedThenRetired fails unless calls holds one migration of old to target made while old was open, and old closed with ErrLinkRetired.
func requireMigratedThenRetired(t *testing.T, calls chan migration, old, target *Link) {
	t.Helper()
	waitFor(t, "the old link to close", func() bool { return old.Err() != nil })
	if !errors.Is(old.Err(), nodesmod.ErrLinkRetired) {
		t.Fatalf("old link closed with %v; want %v", old.Err(), nodesmod.ErrLinkRetired)
	}
	select {
	case m := <-calls:
		if m.old != old.id || m.target != target || m.oldClosed {
			t.Fatalf("migration of %v to %v (old closed %v); want %v to %v before the close", m.old, m.target.id, m.oldClosed, old.id, target.id)
		}
	default:
		t.Fatal("retireLink closed the old link without a migration")
	}
}

// TestRetireClosesWhenLastSessionCloses: U3 AC4. After an upgrade from tor to kcp, the tor link closes once its last session closes.
func TestRetireClosesWhenLastSessionCloses(t *testing.T) {
	mod, remote := newRankedPool(t)
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	mod.retireClock = clock
	calls := recordMigrations(mod)
	tor := rankedLink(t, mod, remote, "tor", true, aged(time.Minute))
	kcp := rankedLink(t, mod, remote, "kcp", false)
	sess := openSession(t, tor, false)

	if !mod.retireAfterUpgrade(tor.id, kcp) {
		t.Fatal("retireAfterUpgrade did not retire the tor link after an upgrade to kcp")
	}
	clock.fire(t)
	waitFor(t, "retireLink to poll again", func() bool { return clock.pendingTimers() > 0 })
	if err := tor.Err(); err != nil {
		t.Fatalf("tor link closed with %v while its session is open", err)
	}

	sess.Close()
	waitFor(t, "the session to leave the mux", func() bool { return tor.mux.sessions.Len() == 0 })
	clock.fire(t)
	requireMigratedThenRetired(t, calls, tor, kcp)
	if elapsed := clock.Now().Sub(time.Unix(1_700_000_000, 0)); elapsed > 2*retirePoll {
		t.Fatalf("tor link closed %v after the upgrade; want within one poll of the session close", elapsed)
	}
}

// TestRetireClosesAtRetireMax: U3 AC4. A session that stays open and is not migratable holds the tor link until retireMax, not longer.
func TestRetireClosesAtRetireMax(t *testing.T) {
	mod, remote := newRankedPool(t)
	start := time.Unix(1_700_000_000, 0)
	clock := &fakeClock{now: start}
	mod.retireClock = clock
	calls := recordMigrations(mod)
	tor := rankedLink(t, mod, remote, "tor", true, aged(time.Minute))
	kcp := rankedLink(t, mod, remote, "kcp", false)
	openSession(t, tor, false)

	if !mod.retireAfterUpgrade(tor.id, kcp) {
		t.Fatal("retireAfterUpgrade did not retire the tor link after an upgrade to kcp")
	}
	for tor.Err() == nil && clock.Now().Sub(start) < retireMax {
		clock.fire(t)
		waitFor(t, "retireLink to poll or close", func() bool { return clock.pendingTimers() > 0 || tor.Err() != nil })
		if tor.Err() != nil && clock.Now().Sub(start) < retireMax {
			t.Fatalf("tor link closed with %v at %v; want it open until retireMax %v", tor.Err(), clock.Now().Sub(start), retireMax)
		}
	}
	requireMigratedThenRetired(t, calls, tor, kcp)
	if elapsed := clock.Now().Sub(start); elapsed != retireMax {
		t.Fatalf("tor link closed at %v; want at retireMax %v", elapsed, retireMax)
	}
}

// TestNoRetireWithoutHigherRank: U3 AC4. An upgrade to a link of equal or lower rank leaves the old link open.
func TestNoRetireWithoutHigherRank(t *testing.T) {
	for _, c := range []struct{ old, target string }{
		{"kcp", "kcp"},
		{"kcp", "tor"},
		{"tcp", "kcp"},
	} {
		t.Run(c.old+" to "+c.target, func(t *testing.T) {
			mod, remote := newRankedPool(t)
			clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
			mod.retireClock = clock
			old := rankedLink(t, mod, remote, c.old, true, aged(time.Minute))
			target := rankedLink(t, mod, remote, c.target, false)

			if mod.retireAfterUpgrade(old.id, target) {
				t.Fatalf("retireAfterUpgrade retired the %v link after an upgrade to %v", c.old, c.target)
			}
			time.Sleep(50 * time.Millisecond)
			if err := old.Err(); err != nil || clock.pendingTimers() != 0 {
				t.Fatalf("old link error %v, %v retire timers; want open and none", err, clock.pendingTimers())
			}
		})
	}
}

// TestRetireLeavesLinkWhenTargetCloses: a retiring link stays open when the link it retires for closes first.
func TestRetireLeavesLinkWhenTargetCloses(t *testing.T) {
	mod, remote := newRankedPool(t)
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	mod.retireClock = clock
	calls := recordMigrations(mod)
	tor := rankedLink(t, mod, remote, "tor", true, aged(time.Minute))
	target := &Link{
		id:        astral.NewNonce(),
		conn:      &rankedConn{local: mod.node.Identity(), remote: remote, network: "kcp"},
		mux:       newMux(mod, channel.New(&recordingLink{}, channel.WithLockedWrites()), mod.node.Identity(), remote, nil, nil),
		done:      make(chan struct{}),
		createdAt: time.Now(),
		idlePing:  time.Hour,
	}
	if err := mod.linkPool.links.Add(target); err != nil {
		t.Fatal(err)
	}
	openSession(t, tor, false)

	if !mod.retireAfterUpgrade(tor.id, target) {
		t.Fatal("retireAfterUpgrade did not retire the tor link")
	}
	waitFor(t, "retireLink to wait", func() bool { return clock.pendingTimers() > 0 })
	target.err.Swap(nil, errTestDead)
	close(target.done)
	time.Sleep(50 * time.Millisecond)
	if err := tor.Err(); err != nil || len(calls) != 0 {
		t.Fatalf("tor link error %v, %v migrations after the target closed; want open and none", err, len(calls))
	}
}

// TestConnectivityUpgradeRetiresTorLink: the pressure upgrade from a tor link to an existing kcp link migrates its sessions and retires the tor link.
func TestConnectivityUpgradeRetiresTorLink(t *testing.T) {
	mod, remote := newRankedPool(t)
	calls := recordMigrations(mod)
	tor := rankedLink(t, mod, remote, "tor", true, aged(time.Minute))
	kcp := rankedLink(t, mod, remote, "kcp", false)

	go mod.connectivityUpgrade(&nodes.LinkPressureEvent{RemoteIdentity: remote, LinkID: tor.id})

	// note: the upgrade migrates first, then the retire finds no session and migrates again before the close.
	select {
	case m := <-calls:
		if m.old != tor.id || m.target != kcp {
			t.Fatalf("upgrade migrated %v to %v; want the tor link to the kcp link", m.old, m.target.id)
		}
	case <-time.After(settleTimeout):
		t.Fatal("the upgrade migrated nothing")
	}
	requireMigratedThenRetired(t, calls, tor, kcp)
	if err := kcp.Err(); err != nil {
		t.Fatalf("kcp link closed: %v", err)
	}
}

// TestCloseExcessMigratesBeforeClose: U3 AC5. The duplicate rule closes the same loser as before; a loser
// holding a migratable session first hands it to the survivor, and stays out of selection meanwhile.
func TestCloseExcessMigratesBeforeClose(t *testing.T) {
	lower, higher := newLinkPair(t)
	type call struct {
		migration
		selected *Link
	}
	calls := make(chan call, 4)
	lower.mod.migrate = func(old astral.Nonce, target *Link) {
		l := lower.mod.getLinkByID(old)
		calls <- call{
			migration: migration{old: old, target: target, oldClosed: l == nil || l.Err() != nil},
			selected:  lower.mod.linkPool.SelectLinkWith(higher.id),
		}
	}

	// note: "higher then lower" of TestDuplicateStrategyLinkPrunedBySameRuleOnBothEnds: the second link survives.
	first := higher.dial(t, lower)
	higher.mod.linkPool.offerStrategyLink(first, nodesmod.StrategyBasic)
	firstAtLower := lower.find(first.id)
	openSession(t, firstAtLower, true)

	second := lower.dial(t, higher)
	lower.mod.linkPool.offerStrategyLink(second, nodesmod.StrategyBasic)

	if got := requireOneSharedLink(t, lower, higher); got != second.id {
		t.Fatalf("both ends kept link %v; want %v", got, second.id)
	}
	requireExcess(t, first, firstAtLower)
	select {
	case c := <-calls:
		if c.old != first.id || c.target != second || c.oldClosed {
			t.Fatalf("migration of %v to %v (old closed %v); want %v to %v before the close", c.old, c.target.id, c.oldClosed, first.id, second.id)
		}
		if c.selected != second {
			t.Fatal("SelectLinkWith returned the excess link while its sessions migrated")
		}
	default:
		t.Fatal("closeExcess closed the loser without a migration")
	}
}

// TestCloseExcessWithoutMigratableSessionClosesAtOnce: a loser with only young sessions closes in the call, with no migration.
func TestCloseExcessWithoutMigratableSessionClosesAtOnce(t *testing.T) {
	mod, remote := newRankedPool(t)
	calls := recordMigrations(mod)
	loser := rankedLink(t, mod, remote, "kcp", false, aged(time.Minute))
	survivor := rankedLink(t, mod, remote, "kcp", true)
	openSession(t, loser, false)

	mod.linkPool.closeExcess(loser, survivor)
	if !errors.Is(loser.Err(), nodesmod.ErrExcessLink) || len(calls) != 0 {
		t.Fatalf("loser error %v, %v migrations; want %v at once and none", loser.Err(), len(calls), nodesmod.ErrExcessLink)
	}
}
