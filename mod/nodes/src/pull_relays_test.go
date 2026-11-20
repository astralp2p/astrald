package nodes

import (
	"iter"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	authmod "github.com/astralp2p/astrald/mod/auth"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// pullUser is a user module whose siblings answer fetch, counting its calls.
// Every identity is a swarm member except those in expelled.
type pullUser struct {
	usermod.Module
	id       *astral.Identity
	swarm    []*astral.Identity
	expelled []*astral.Identity
	fetches  atomic.Int32
	// incomplete makes the fetch report that not every sibling answered.
	incomplete atomic.Bool
	// fetch yields what the siblings answer for issuer; nil yields nothing.
	fetch func(ctx *astral.Context, issuer *astral.Identity, yield func(*astral.Identity, *auth.SignedContract) bool)
}

func (u *pullUser) Identity() *astral.Identity     { return u.id }
func (u *pullUser) LocalSwarm() []*astral.Identity { return u.swarm }
func (u *pullUser) IsSwarmMember(id *astral.Identity) bool {
	return !slices.ContainsFunc(u.expelled, id.IsEqual)
}
func (u *pullUser) FetchRelayContracts(ctx *astral.Context, issuer *astral.Identity) iter.Seq2[*astral.Identity, *auth.SignedContract] {
	seq, _ := u.FetchRelayContractsReport(ctx, issuer)
	return seq
}
func (u *pullUser) FetchRelayContractsReport(ctx *astral.Context, issuer *astral.Identity) (iter.Seq2[*astral.Identity, *auth.SignedContract], func() bool) {
	u.fetches.Add(1)
	seq := func(yield func(*astral.Identity, *auth.SignedContract) bool) {
		if u.fetch != nil {
			u.fetch(ctx, issuer, yield)
		}
	}
	return seq, func() bool { return !u.incomplete.Load() }
}

// pullIndex is an auth module holding an in-memory index of active contracts.
// A contract in refused is not admitted; a contract in withdrawn is held but
// no query finds it.
type pullIndex struct {
	authmod.Module
	mu        sync.Mutex
	index     []*auth.SignedContract
	refused   map[*auth.SignedContract]bool
	withdrawn map[*auth.SignedContract]bool
	// stall makes IndexContract block until its context ends.
	stall bool
}

func (a *pullIndex) IndexContract(ctx *astral.Context, sc *auth.SignedContract) error {
	if a.stall {
		<-ctx.Done()
		return ctx.Err()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.refused[sc] {
		return authmod.ErrNotAdmitted
	}
	for _, held := range a.index {
		if held == sc {
			return nil
		}
	}
	a.index = append(a.index, sc)
	return nil
}

func (a *pullIndex) SignedContracts() authmod.ContractQueryBuilder {
	return &pullQuery{index: a}
}

type pullQuery struct {
	authmod.ContractQueryBuilder
	index  *pullIndex
	issuer *astral.Identity
}

func (q *pullQuery) WithIssuer(id *astral.Identity) authmod.ContractQueryBuilder {
	q.issuer = id
	return q
}

func (q *pullQuery) WithAction(...astral.Object) authmod.ContractQueryBuilder { return q }

func (q *pullQuery) Find(*astral.Context) (list []*auth.SignedContract, _ error) {
	q.index.mu.Lock()
	defer q.index.mu.Unlock()
	for _, sc := range q.index.index {
		if sc.Issuer.IsEqual(q.issuer) && !q.index.withdrawn[sc] {
			list = append(list, sc)
		}
	}
	return list, nil
}

// pullFixture is a nodes module on nodeID whose user and auth are fakes, with a
// settable clock.
type pullFixture struct {
	mod    *Module
	nodeID *astral.Identity
	user   *pullUser
	index  *pullIndex
	now    time.Time
	mu     sync.Mutex
}

func newPullFixture(t *testing.T) *pullFixture {
	setPullBudgets(t, testPullWait, testPullFlight)
	f := &pullFixture{
		nodeID: astral.GenerateIdentity(),
		user:   &pullUser{id: astral.GenerateIdentity()},
		index:  &pullIndex{refused: map[*auth.SignedContract]bool{}, withdrawn: map[*auth.SignedContract]bool{}},
		now:    time.Unix(1800000000, 0),
	}
	f.mod = &Module{
		Deps: Deps{User: f.user, Auth: f.index},
		node: &identityNode{id: f.nodeID},
		log:  log.New(f.nodeID),
		ctx:  networkCtx(),
	}
	f.mod.pull.now = f.clock
	return f
}

// The budgets the fixtures run with: the caller waits testPullWait, the
// flight lasts testPullFlight.
const (
	testPullWait   = 300 * time.Millisecond
	testPullFlight = 1 * time.Second
)

// setPullBudgets sets the pull budgets for the test and restores them after.
// note: the budgets are package variables, so tests that set them never run in parallel.
func setPullBudgets(t *testing.T, wait, flight time.Duration) {
	t.Helper()
	oldWait, oldFlight := pullWait, pullFlightTimeout
	pullWait, pullFlightTimeout = wait, flight
	t.Cleanup(func() { pullWait, pullFlightTimeout = oldWait, oldFlight })
}

// setPullIndexTimeout sets the index step budget for the test and restores it after.
func setPullIndexTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := pullIndexTimeout
	pullIndexTimeout = d
	t.Cleanup(func() { pullIndexTimeout = old })
}

// awaitFlightEnd waits until no flight runs for target and returns when it ended.
func (f *pullFixture) awaitFlightEnd(t *testing.T, target *astral.Identity) time.Time {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, running := f.mod.pull.flights.Get(target.String()); !running {
			return time.Now()
		}
		if time.Now().After(deadline) {
			t.Fatal("the flight never ended")
		}
		time.Sleep(time.Millisecond)
	}
}

// missed reports whether the fixture holds a miss for target, expired or not.
func (f *pullFixture) missed(target *astral.Identity) bool {
	_, ok := f.mod.pull.misses.Get(target.String())
	return ok
}

// slowly returns a fetch in which sender answers contracts after delay, unless
// the ask's context ends first.
func slowly(delay time.Duration, sender *astral.Identity, contracts ...*auth.SignedContract) func(*astral.Context, *astral.Identity, func(*astral.Identity, *auth.SignedContract) bool) {
	return func(ctx *astral.Context, issuer *astral.Identity, yield func(*astral.Identity, *auth.SignedContract) bool) {
		select {
		case <-time.After(delay):
			answering(sender, contracts...)(ctx, issuer, yield)
		case <-ctx.Done():
		}
	}
}

func (f *pullFixture) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *pullFixture) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func networkCtx() *astral.Context {
	return astral.NewContext(nil).IncludeZone(astral.ZoneNetwork)
}

func relayContractOf(issuer, subject *astral.Identity) *auth.SignedContract {
	return &auth.SignedContract{Contract: &auth.Contract{
		Issuer:  issuer,
		Subject: subject,
		Permits: []*auth.Permit{{Action: astral.String8(nodes.RelayForAction{}.ObjectType())}},
	}}
}

// answering returns a fetch in which sender answers contracts.
func answering(sender *astral.Identity, contracts ...*auth.SignedContract) func(*astral.Context, *astral.Identity, func(*astral.Identity, *auth.SignedContract) bool) {
	return func(_ *astral.Context, _ *astral.Identity, yield func(*astral.Identity, *auth.SignedContract) bool) {
		for _, sc := range contracts {
			if !yield(sender, sc) {
				return
			}
		}
	}
}

func assertRelays(t *testing.T, got []*astral.Identity, want ...*astral.Identity) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d relays %v; want %v", len(got), got, want)
	}
	for i := range want {
		if !got[i].IsEqual(want[i]) {
			t.Fatalf("relay %d is %v; want %v", i, got[i], want[i])
		}
	}
}

// A pulled contract that the index admits names its subject as the relay.
func TestPullRelaysIndexesAndRoutesThroughTheSubject(t *testing.T) {
	f := newPullFixture(t)
	app, host := astral.GenerateIdentity(), astral.GenerateIdentity()
	sc := relayContractOf(app, host)
	f.user.fetch = answering(host, sc)

	assertRelays(t, f.mod.pullRelays(networkCtx(), app), host)
	if len(f.index.index) != 1 || f.index.index[0] != sc {
		t.Fatalf("index holds %v; want the pulled contract", f.index.index)
	}
}

// The gate: no ask for this node, a local swarm member, the user, or a target
// with a cached miss.
func TestPullRelaysGate(t *testing.T) {
	f := newPullFixture(t)
	member := astral.GenerateIdentity()
	f.user.swarm = []*astral.Identity{f.nodeID, member}

	for name, target := range map[string]*astral.Identity{
		"this node": f.nodeID,
		"a member":  member,
		"the user":  f.user.id,
	} {
		if relays := f.mod.pullRelays(networkCtx(), target); relays != nil {
			t.Errorf("%s: got relays %v; want none", name, relays)
		}
	}
	if n := f.user.fetches.Load(); n != 0 {
		t.Fatalf("asked the siblings %d times for gated targets; want 0", n)
	}

	absent := astral.GenerateIdentity()
	f.mod.pullRelays(networkCtx(), absent)
	f.mod.pullRelays(networkCtx(), absent)
	if n := f.user.fetches.Load(); n != 1 {
		t.Fatalf("asked the siblings %d times for an absent target, twice; want 1 (the miss is cached)", n)
	}
}

// The miss expires after pullMissTTL on the module's clock, and not before.
func TestPullRelaysMissExpires(t *testing.T) {
	f := newPullFixture(t)
	absent := astral.GenerateIdentity()

	f.mod.pullRelays(networkCtx(), absent)
	if !f.mod.isPullGated(absent) {
		t.Fatal("a complete empty ask left the target ungated")
	}
	f.advance(pullMissTTL - time.Second)
	f.mod.pullRelays(networkCtx(), absent)
	if n := f.user.fetches.Load(); n != 1 {
		t.Fatalf("asked %d times within the miss TTL; want 1", n)
	}

	f.advance(time.Second)
	if f.mod.isPullGated(absent) {
		t.Fatal("the target is still gated once the miss expired")
	}
	f.mod.pullRelays(networkCtx(), absent)
	if n := f.user.fetches.Load(); n != 2 {
		t.Fatalf("asked %d times once the miss expired; want 2", n)
	}
}

// Concurrent misses for one target share one ask, and every one of them gets
// its relays.
func TestPullRelaysSingleFlight(t *testing.T) {
	f := newPullFixture(t)
	app, host := astral.GenerateIdentity(), astral.GenerateIdentity()
	release := make(chan struct{})
	f.user.fetch = func(_ *astral.Context, _ *astral.Identity, yield func(*astral.Identity, *auth.SignedContract) bool) {
		<-release
		yield(host, relayContractOf(app, host))
	}

	const callers = 8
	results := make(chan []*astral.Identity, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- f.mod.pullRelays(networkCtx(), app)
		}()
	}

	// why the wait: every caller must be waiting on the ask before it ends.
	deadline := time.Now().Add(5 * time.Second)
	for f.user.fetches.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	close(results)

	if n := f.user.fetches.Load(); n != 1 {
		t.Fatalf("%d concurrent misses asked the siblings %d times; want 1", callers, n)
	}
	for relays := range results {
		assertRelays(t, relays, host)
	}
}

// The ask's own queries target siblings; a miss for a sibling during an ask
// starts no nested ask.
func TestPullRelaysNoRecursion(t *testing.T) {
	f := newPullFixture(t)
	sibling := astral.GenerateIdentity()
	f.user.swarm = []*astral.Identity{f.nodeID, sibling}
	app := astral.GenerateIdentity()

	var nested []*astral.Identity
	f.user.fetch = func(ctx *astral.Context, _ *astral.Identity, _ func(*astral.Identity, *auth.SignedContract) bool) {
		// the ask routes user.relay_contracts to sibling, and that routing misses
		nested = f.mod.pullRelays(ctx, sibling)
	}

	f.mod.pullRelays(networkCtx(), app)
	if n := f.user.fetches.Load(); n != 1 {
		t.Fatalf("asked the siblings %d times; want 1 (no nested ask)", n)
	}
	if nested != nil {
		t.Fatalf("the nested miss got relays %v; want none", nested)
	}
}

// A contract the index refuses, or holds as withdrawn, yields no relay and a miss.
func TestPullRelaysRefusedOrWithdrawnYieldsNoRelay(t *testing.T) {
	for _, name := range []string{"refused", "withdrawn"} {
		t.Run(name, func(t *testing.T) {
			f := newPullFixture(t)
			app, host := astral.GenerateIdentity(), astral.GenerateIdentity()
			sc := relayContractOf(app, host)
			if name == "refused" {
				f.index.refused[sc] = true
			} else {
				f.index.index = append(f.index.index, sc)
				f.index.withdrawn[sc] = true
			}
			f.user.fetch = answering(host, sc)

			if relays := f.mod.pullRelays(networkCtx(), app); relays != nil {
				t.Fatalf("got relays %v; want none", relays)
			}
			f.mod.pullRelays(networkCtx(), app)
			if n := f.user.fetches.Load(); n != 1 {
				t.Fatalf("asked %d times; want 1 (the miss is cached)", n)
			}
		})
	}
}

// The ask stops at the first contract that gives a relay: a later answer is
// never read.
func TestPullRelaysStopsAtTheFirstRelay(t *testing.T) {
	f := newPullFixture(t)
	app, host, other := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	f.user.fetch = answering(host, relayContractOf(app, host), relayContractOf(app, other))

	assertRelays(t, f.mod.pullRelays(networkCtx(), app), host)
	if len(f.index.index) != 1 {
		t.Fatalf("indexed %d contracts; want 1", len(f.index.index))
	}
}

// A silent sibling holds the caller no longer than pullWait: an answering
// sibling's contract routes at once, and a target a sibling never answers for
// returns nil when the caller's wait ends, before the flight does.
func TestPullRelaysBoundedBySilentSiblings(t *testing.T) {
	f := newPullFixture(t)
	app, host := astral.GenerateIdentity(), astral.GenerateIdentity()
	sc := relayContractOf(app, host)
	f.user.fetch = func(ctx *astral.Context, issuer *astral.Identity, yield func(*astral.Identity, *auth.SignedContract) bool) {
		if issuer.IsEqual(app) && !yield(host, sc) {
			return
		}
		<-ctx.Done() // the silent sibling
	}

	start := time.Now()
	assertRelays(t, f.mod.pullRelays(networkCtx(), app), host)
	if d := time.Since(start); d > pullWait {
		t.Fatalf("an answered ask took %v; want under %v", d, pullWait)
	}

	silent := astral.GenerateIdentity()
	start = time.Now()
	if relays := f.mod.pullRelays(networkCtx(), silent); relays != nil {
		t.Fatalf("got relays %v for a target nobody answers", relays)
	}
	if d := time.Since(start); d < pullWait || d > pullWait+(pullFlightTimeout-pullWait)/2 {
		t.Fatalf("an unanswered ask held the caller %v; want about %v, under the flight %v", d, pullWait, pullFlightTimeout)
	}
}

// The fetch answers before pullWait: the caller gets the relays and no miss is
// recorded.
func TestPullRelaysAnswerBeforeTheWaitRoutes(t *testing.T) {
	f := newPullFixture(t)
	setPullBudgets(t, 100*time.Millisecond, 400*time.Millisecond)
	app, host := astral.GenerateIdentity(), astral.GenerateIdentity()
	f.user.fetch = slowly(10*time.Millisecond, host, relayContractOf(app, host))

	assertRelays(t, f.mod.pullRelays(networkCtx(), app), host)
	if f.missed(app) || f.mod.isPullGated(app) {
		t.Fatal("an answered ask recorded a miss")
	}
}

// The fetch answers after pullWait and before pullFlightTimeout: the caller
// gets nil when its wait ends, the flight still indexes the contract, no miss
// is recorded, and the next pull returns the relay without asking again.
func TestPullRelaysLateAnswerIsIndexedAndReused(t *testing.T) {
	f := newPullFixture(t)
	setPullBudgets(t, 100*time.Millisecond, 600*time.Millisecond)
	app, host := astral.GenerateIdentity(), astral.GenerateIdentity()
	sc := relayContractOf(app, host)
	f.user.fetch = slowly(300*time.Millisecond, host, sc)

	start := time.Now()
	if relays := f.mod.pullRelays(networkCtx(), app); relays != nil {
		t.Fatalf("got relays %v after the wait; want nil", relays)
	}
	if d := time.Since(start); d >= 300*time.Millisecond {
		t.Fatalf("the caller waited %v; want it released at the wait %v", d, pullWait)
	}

	f.awaitFlightEnd(t, app)
	if len(f.index.index) != 1 || f.index.index[0] != sc {
		t.Fatalf("index holds %v; want the late contract", f.index.index)
	}
	if f.missed(app) || f.mod.isPullGated(app) {
		t.Fatal("a late answer left a miss or a gate")
	}

	assertRelays(t, f.mod.pullRelays(networkCtx(), app), host)
	if n := f.user.fetches.Load(); n != 1 {
		t.Fatalf("asked the siblings %d times; want 1 (the next pull reads the index)", n)
	}
}

// A caller leaves at the wait while the flight runs on; a second caller then
// joins the same flight, starts no ask, and gets the late contract.
func TestPullRelaysCallerJoinsTheFlightAfterAnotherLeft(t *testing.T) {
	f := newPullFixture(t)
	setPullBudgets(t, 150*time.Millisecond, 2*time.Second)
	app, host := astral.GenerateIdentity(), astral.GenerateIdentity()
	release := make(chan struct{})
	f.user.fetch = func(ctx *astral.Context, _ *astral.Identity, yield func(*astral.Identity, *auth.SignedContract) bool) {
		select {
		case <-release:
			yield(host, relayContractOf(app, host))
		case <-ctx.Done():
		}
	}

	if relays := f.mod.pullRelays(networkCtx(), app); relays != nil {
		t.Fatalf("the first caller got relays %v at the wait; want nil", relays)
	}
	if _, running := f.mod.pull.flights.Get(app.String()); !running {
		t.Fatal("the flight ended with the first caller's wait")
	}

	second := make(chan []*astral.Identity, 1)
	go func() { second <- f.mod.pullRelays(networkCtx(), app) }()
	// why the pause: the second caller must be waiting on the flight, not past it.
	time.Sleep(20 * time.Millisecond)
	select {
	case relays := <-second:
		t.Fatalf("the second caller got %v before the contract arrived", relays)
	default:
	}
	close(release)

	select {
	case relays := <-second:
		assertRelays(t, relays, host)
	case <-time.After(5 * time.Second):
		t.Fatal("the second caller never got the late contract")
	}
	if n := f.user.fetches.Load(); n != 1 {
		t.Fatalf("the callers asked the siblings %d times; want 1 (one shared flight)", n)
	}
	if f.missed(app) || f.mod.isPullGated(app) {
		t.Fatal("a shared flight left a miss or a gate")
	}
}

// A contract that arrives inside pullFlightTimeout is indexed on a fresh
// pullIndexTimeout context, so the flight leaves pull.flights within
// pullFlightTimeout + pullIndexTimeout, and no later. The stalled index makes
// the step run to its full budget.
func TestPullRelaysFlightLeavesWithinTheFlightAndIndexBudgets(t *testing.T) {
	f := newPullFixture(t)
	setPullBudgets(t, 50*time.Millisecond, 400*time.Millisecond)
	setPullIndexTimeout(t, 300*time.Millisecond)
	f.index.stall = true
	app, host := astral.GenerateIdentity(), astral.GenerateIdentity()
	// the contract arrives just inside the flight
	f.user.fetch = slowly(pullFlightTimeout-50*time.Millisecond, host, relayContractOf(app, host))

	start := time.Now()
	if relays := f.mod.pullRelays(networkCtx(), app); relays != nil {
		t.Fatalf("got relays %v; want nil", relays)
	}
	ended := f.awaitFlightEnd(t, app).Sub(start)

	if ended < pullFlightTimeout {
		t.Fatalf("the flight ended after %v; want it to stay for the index step past %v", ended, pullFlightTimeout)
	}
	if limit := pullFlightTimeout + pullIndexTimeout + 250*time.Millisecond; ended > limit {
		t.Fatalf("the flight ended after %v; want within %v + %v", ended, pullFlightTimeout, pullIndexTimeout)
	}
	if f.missed(app) {
		t.Fatal("a flight whose index step failed recorded a miss")
	}
}

// The fetch never answers: the flight ends at pullFlightTimeout, records no
// miss, and leaves the target ungated, although the fetch reports complete.
func TestPullRelaysBudgetExpiryRecordsNoMiss(t *testing.T) {
	f := newPullFixture(t)
	setPullBudgets(t, 50*time.Millisecond, 200*time.Millisecond)
	app := astral.GenerateIdentity()
	f.user.fetch = func(ctx *astral.Context, _ *astral.Identity, _ func(*astral.Identity, *auth.SignedContract) bool) {
		<-ctx.Done()
	}

	start := time.Now()
	if relays := f.mod.pullRelays(networkCtx(), app); relays != nil {
		t.Fatalf("got relays %v; want nil", relays)
	}
	ended := f.awaitFlightEnd(t, app)
	if d := ended.Sub(start); d < pullFlightTimeout {
		t.Fatalf("the flight ended after %v; want it to run to %v", d, pullFlightTimeout)
	}
	if f.missed(app) || f.mod.isPullGated(app) {
		t.Fatal("a flight that ran out of budget recorded a miss")
	}
}

// The module ending cuts a running flight: the fetch sees its context end, the
// flight leaves promptly, and no miss is recorded although the fetch reports
// complete.
func TestPullRelaysModuleShutdownEndsTheFlight(t *testing.T) {
	f := newPullFixture(t)
	setPullBudgets(t, 50*time.Millisecond, 10*time.Second)
	modCtx, stop := networkCtx().WithCancel()
	defer stop()
	f.mod.ctx = modCtx
	app := astral.GenerateIdentity()
	f.user.fetch = func(ctx *astral.Context, _ *astral.Identity, _ func(*astral.Identity, *auth.SignedContract) bool) {
		<-ctx.Done()
	}

	if relays := f.mod.pullRelays(networkCtx(), app); relays != nil {
		t.Fatalf("got relays %v; want nil", relays)
	}
	if _, running := f.mod.pull.flights.Get(app.String()); !running {
		t.Fatal("the flight ended before the module did")
	}

	stopped := time.Now()
	stop()
	ended := f.awaitFlightEnd(t, app)
	if d := ended.Sub(stopped); d > time.Second {
		t.Fatalf("the flight outlived the module by %v; want it to end promptly", d)
	}
	if f.missed(app) || f.mod.isPullGated(app) {
		t.Fatal("a flight cut by the module's shutdown recorded a miss")
	}
}

// An ask that is incomplete (a sibling's ask failed) and yields no contract
// records no miss, so the next pull asks again.
func TestPullRelaysIncompleteAskRecordsNoMiss(t *testing.T) {
	f := newPullFixture(t)
	app := astral.GenerateIdentity()
	f.user.incomplete.Store(true)

	if relays := f.mod.pullRelays(networkCtx(), app); relays != nil {
		t.Fatalf("got relays %v; want nil", relays)
	}
	if f.missed(app) || f.mod.isPullGated(app) {
		t.Fatal("an incomplete ask recorded a miss")
	}
	f.mod.pullRelays(networkCtx(), app)
	if n := f.user.fetches.Load(); n != 2 {
		t.Fatalf("asked the siblings %d times; want 2 (no miss gates the second)", n)
	}
}

// A complete empty ask records a miss that gates the target until pullMissTTL
// passes on the module's clock.
func TestPullRelaysCompleteEmptyAskRecordsAMiss(t *testing.T) {
	f := newPullFixture(t)
	app := astral.GenerateIdentity()

	if relays := f.mod.pullRelays(networkCtx(), app); relays != nil {
		t.Fatalf("got relays %v; want nil", relays)
	}
	if !f.missed(app) || !f.mod.isPullGated(app) {
		t.Fatal("a complete empty ask recorded no miss")
	}
	f.advance(pullMissTTL - time.Millisecond)
	if !f.mod.isPullGated(app) {
		t.Fatal("the miss expired before pullMissTTL")
	}
	f.advance(time.Millisecond)
	if f.mod.isPullGated(app) {
		t.Fatal("the miss outlived pullMissTTL")
	}
}

// Two callers during one running flight share it: the fetch runs once and both
// get the relays.
func TestPullRelaysTwoCallersShareOneFlight(t *testing.T) {
	f := newPullFixture(t)
	app, host := astral.GenerateIdentity(), astral.GenerateIdentity()
	f.user.fetch = slowly(100*time.Millisecond, host, relayContractOf(app, host))

	results := make(chan []*astral.Identity, 2)
	for range 2 {
		go func() { results <- f.mod.pullRelays(networkCtx(), app) }()
	}
	for range 2 {
		assertRelays(t, <-results, host)
	}
	if n := f.user.fetches.Load(); n != 1 {
		t.Fatalf("two callers asked the siblings %d times; want 1", n)
	}
}

// A caller whose context ends stops waiting, and the shared ask still records
// its outcome for the next miss.
func TestPullRelaysCallerCancelDoesNotCutTheAsk(t *testing.T) {
	f := newPullFixture(t)
	app, host := astral.GenerateIdentity(), astral.GenerateIdentity()
	release := make(chan struct{})
	// the siblings answer once released, unless the ask's context ends first
	f.user.fetch = func(ctx *astral.Context, _ *astral.Identity, yield func(*astral.Identity, *auth.SignedContract) bool) {
		select {
		case <-release:
			yield(host, relayContractOf(app, host))
		case <-ctx.Done():
		}
	}

	ctx, cancel := networkCtx().WithCancel()
	cancel()
	if relays := f.mod.pullRelays(ctx, app); relays != nil {
		t.Fatalf("a cancelled caller got relays %v", relays)
	}
	close(release)

	deadline := time.Now().Add(5 * time.Second)
	for len(f.mod.heldRelays(networkCtx(), app)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the ask a cancelled caller started never indexed the contract")
		}
		time.Sleep(time.Millisecond)
	}
	if _, missed := f.mod.pull.misses.Get(app.String()); missed {
		t.Fatal("a cancelled caller recorded a miss")
	}
}

// A claimed node takes no former member as a relay: a relay contract whose
// subject was expelled, held in the index or answered by a sibling, yields no
// relay and a miss, so the query fails as route not found.
func TestPullRelaysSkipsAnExpelledSubject(t *testing.T) {
	f := newPullFixture(t)
	app, expelled := astral.GenerateIdentity(), astral.GenerateIdentity()
	f.user.expelled = []*astral.Identity{expelled}
	sc := relayContractOf(app, expelled)
	f.index.index = append(f.index.index, sc)
	f.user.fetch = answering(astral.GenerateIdentity(), sc)

	if relays := f.mod.pullRelays(networkCtx(), app); relays != nil {
		t.Fatalf("got relays %v; want none for an expelled subject", relays)
	}
	if _, missed := f.mod.pull.misses.Get(app.String()); !missed {
		t.Fatal("an ask that left only an expelled subject recorded no miss")
	}

	member := astral.GenerateIdentity()
	f.index.index = append(f.index.index, relayContractOf(app, member))
	assertRelays(t, f.mod.heldRelays(networkCtx(), app), member)
}
