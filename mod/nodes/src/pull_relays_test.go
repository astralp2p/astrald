package nodes

import (
	"iter"
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
type pullUser struct {
	usermod.Module
	id      *astral.Identity
	swarm   []*astral.Identity
	fetches atomic.Int32
	// fetch yields what the siblings answer for issuer; nil yields nothing.
	fetch func(ctx *astral.Context, issuer *astral.Identity, yield func(*astral.Identity, *auth.SignedContract) bool)
}

func (u *pullUser) Identity() *astral.Identity     { return u.id }
func (u *pullUser) LocalSwarm() []*astral.Identity { return u.swarm }
func (u *pullUser) FetchRelayContracts(ctx *astral.Context, issuer *astral.Identity) iter.Seq2[*astral.Identity, *auth.SignedContract] {
	u.fetches.Add(1)
	return func(yield func(*astral.Identity, *auth.SignedContract) bool) {
		if u.fetch != nil {
			u.fetch(ctx, issuer, yield)
		}
	}
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
}

func (a *pullIndex) IndexContract(_ *astral.Context, sc *auth.SignedContract) error {
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

func newPullFixture() *pullFixture {
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
	}
	f.mod.pull.now = f.clock
	return f
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
	f := newPullFixture()
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
	f := newPullFixture()
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
	f := newPullFixture()
	absent := astral.GenerateIdentity()

	f.mod.pullRelays(networkCtx(), absent)
	f.advance(pullMissTTL - time.Second)
	f.mod.pullRelays(networkCtx(), absent)
	if n := f.user.fetches.Load(); n != 1 {
		t.Fatalf("asked %d times within the miss TTL; want 1", n)
	}

	f.advance(time.Second)
	f.mod.pullRelays(networkCtx(), absent)
	if n := f.user.fetches.Load(); n != 2 {
		t.Fatalf("asked %d times once the miss expired; want 2", n)
	}
}

// Concurrent misses for one target share one ask, and every one of them gets
// its relays.
func TestPullRelaysSingleFlight(t *testing.T) {
	f := newPullFixture()
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
	f := newPullFixture()
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
			f := newPullFixture()
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
	f := newPullFixture()
	app, host, other := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	f.user.fetch = answering(host, relayContractOf(app, host), relayContractOf(app, other))

	assertRelays(t, f.mod.pullRelays(networkCtx(), app), host)
	if len(f.index.index) != 1 {
		t.Fatalf("indexed %d contracts; want 1", len(f.index.index))
	}
}

// A silent sibling holds the ask no longer than pullTimeout: an answering
// sibling's contract routes at once, and a target nobody answers fails within
// the timeout.
func TestPullRelaysBoundedBySilentSiblings(t *testing.T) {
	f := newPullFixture()
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
	if d := time.Since(start); d > pullTimeout {
		t.Fatalf("an answered ask took %v; want under %v", d, pullTimeout)
	}

	start = time.Now()
	if relays := f.mod.pullRelays(networkCtx(), astral.GenerateIdentity()); relays != nil {
		t.Fatalf("got relays %v for a target nobody answers", relays)
	}
	if d := time.Since(start); d < pullTimeout || d > pullTimeout+500*time.Millisecond {
		t.Fatalf("an unanswered ask took %v; want about %v", d, pullTimeout)
	}
}

// A caller whose context ends stops waiting, and the shared ask still records
// its outcome for the next miss.
func TestPullRelaysCallerCancelDoesNotCutTheAsk(t *testing.T) {
	f := newPullFixture()
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
