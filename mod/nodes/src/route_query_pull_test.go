package nodes

import (
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// routedQuery is one query a host's router received over a link.
type routedQuery struct {
	caller, target *astral.Identity
}

// hostRouter is the router of a host node: it records each query it receives
// for an app it hosts and answers it with reply.
type hostRouter struct {
	reply  string
	mu     sync.Mutex
	routed []routedQuery
}

func (r *hostRouter) RouteQuery(_ *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	r.record(q)
	go func() {
		w.Write([]byte(r.reply))
		w.Close()
	}()
	return nopWriteCloser{}, nil
}

func (r *hostRouter) record(q *astral.InFlightQuery) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routed = append(r.routed, routedQuery{caller: q.Caller, target: q.Target})
}

func (r *hostRouter) received() []routedQuery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]routedQuery(nil), r.routed...)
}

// newRoutingFixture is a pull fixture whose module routes queries: a link pool
// with no link strategies, so a link to an app is never produced, and an
// endpoint DB without endpoints.
func newRoutingFixture(t *testing.T) *pullFixture {
	t.Helper()
	f := newPullFixture()
	f.mod.db = testEndpointDB(t)
	f.mod.ctx = astral.NewContext(nil)
	// note: the pool is never closed; its links are bare muxes whose pipes the test closes.
	f.mod.linkPool = NewLinkPool(f.mod)
	return f
}

// link adds a live link from the fixture's node to host, whose end of the
// link hands every query to router.
func (f *pullFixture) link(t *testing.T, host *astral.Identity, router astral.Router) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() {
		a.Close()
		b.Close()
	})
	local := testMux(t, a, f.nodeID, host)
	remote := testMux(t, b, host, f.nodeID)
	remote.SetRouter(router)
	link := &Link{id: astral.NewNonce(), conn: &identityConn{remote: host}, mux: local}
	if err := f.mod.linkPool.links.Add(link); err != nil {
		t.Fatalf("add link: %v", err)
	}
}

// routeResult is what the caller of one routed query got.
type routeResult struct {
	answer string        // what the caller read
	err    error         // the routing error, or the answer's timeout
	took   time.Duration // how long RouteQuery took
}

// routeFromNode routes one query from the fixture's node to target with no
// relay in q.Extra, as a query whose relay contract never arrived.
//
// note: safe to call from a goroutine; it never fails the test itself.
func (f *pullFixture) routeFromNode(target *astral.Identity) (r routeResult) {
	ctx, cancel := networkCtx().WithIdentity(f.nodeID).WithTimeout(effectTimeout)
	defer cancel()
	q := astral.Launch(&astral.Query{
		Nonce:       astral.NewNonce(),
		Caller:      f.nodeID,
		Target:      target,
		QueryString: "app.op",
	})

	pr, pw := io.Pipe()
	start := time.Now()
	_, r.err = f.mod.RouteQuery(ctx, q, pw)
	r.took = time.Since(start)
	if r.err != nil {
		return r
	}

	answer := make(chan string, 1)
	go func() {
		buf, _ := io.ReadAll(pr)
		answer <- string(buf)
	}()
	select {
	case r.answer = <-answer:
	case <-time.After(effectTimeout):
		r.err = errors.New("the answer never reached the caller")
	}
	return r
}

// The registration push of app X from host A to node B is dropped: B holds no
// relay contract for X and q.Extra lists no relay. B's first query to X asks
// the linked siblings, routes through A within 1.5 s, and X's contract is in
// B's index afterwards.
// Covers tyUYbuZqVI acceptance criterion 4 in process.
func TestRouteQueryAfterADroppedPushRoutesThroughTheHost(t *testing.T) {
	f := newRoutingFixture(t)
	app, host := astral.GenerateIdentity(), astral.GenerateIdentity()
	f.user.swarm = []*astral.Identity{f.nodeID, host}
	router := &hostRouter{reply: "hello"}
	f.link(t, host, router)
	sc := relayContractOf(app, host)
	f.user.fetch = answering(host, sc)

	r := f.routeFromNode(app)
	if r.err != nil {
		t.Fatalf("the first query to the app answered %v; want it routed through the host", r.err)
	}
	if r.answer != "hello" {
		t.Fatalf("caller read %q; want the app's answer", r.answer)
	}
	if r.took >= 1500*time.Millisecond {
		t.Fatalf("the first query took %v; want under 1.5 s", r.took)
	}
	if routed := router.received(); len(routed) != 1 || !routed[0].target.IsEqual(app) || !routed[0].caller.IsEqual(f.nodeID) {
		t.Fatalf("the host routed %v; want one query from this node to the app", routed)
	}
	if n := f.user.fetches.Load(); n != 1 {
		t.Fatalf("asked the siblings %d times; want 1", n)
	}
	assertRelays(t, f.mod.heldRelays(networkCtx(), app), host)
}

// A query to an app identity no sibling holds a contract for answers route not
// found within 1.1 s, also when a sibling never answers; a second query within
// 30 s answers the same without a second ask.
// Covers tyUYbuZqVI acceptance criterion 5 in process.
func TestRouteQueryToAnAbsentAppFailsFastAndAsksOnce(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fetch func(*astral.Context, *astral.Identity, func(*astral.Identity, *auth.SignedContract) bool)
	}{
		{"siblings answer none", nil},
		{"a sibling is silent", func(ctx *astral.Context, _ *astral.Identity, _ func(*astral.Identity, *auth.SignedContract) bool) {
			select {
			case <-ctx.Done():
			case <-time.After(effectTimeout):
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRoutingFixture(t)
			sibling := astral.GenerateIdentity()
			f.user.swarm = []*astral.Identity{f.nodeID, sibling}
			f.link(t, sibling, &hostRouter{})
			f.user.fetch = tc.fetch
			absent := astral.GenerateIdentity()

			r := f.routeFromNode(absent)
			assertRouteNotFound(t, r.err)
			if r.took >= 1100*time.Millisecond {
				t.Fatalf("route not found took %v; want under 1.1 s", r.took)
			}

			f.advance(pullMissTTL - time.Millisecond)
			r = f.routeFromNode(absent)
			assertRouteNotFound(t, r.err)
			if r.took >= pullTimeout {
				t.Fatalf("the second query took %v; want it to fail without an ask", r.took)
			}
			if n := f.user.fetches.Load(); n != 1 {
				t.Fatalf("asked the siblings %d times within 30 s; want 1", n)
			}
		})
	}
}

// relayPushDelay is how long a slowed registration push takes to reach a
// sibling: longer than a discovery update, so every first query races it.
const relayPushDelay = 300 * time.Millisecond

// The report's scenario: four linked siblings each host one provider app, and
// each provider's relay-contract push is slowed past the discovery update.
// Every node queries each remote provider once, at once, with no retry: 12 of
// 12 pairs answer through the provider's host with no route not found, each
// under 1.5 s, and each consumer asks once per remote provider.
// Covers zuIhqtHhEG acceptance criterion 4 in process.
func TestRouteQueryFirstCallsWinTheRelayContractRace(t *testing.T) {
	const size = 4
	members := make([]*pullFixture, size)
	routers := make([]*hostRouter, size)
	apps := make([]*astral.Identity, size)
	contracts := make([]*auth.SignedContract, size)
	for i := range members {
		members[i] = newRoutingFixture(t)
		routers[i] = &hostRouter{reply: "echo"}
		apps[i] = astral.GenerateIdentity()
		contracts[i] = relayContractOf(apps[i], members[i].nodeID)
	}

	swarm := make([]*astral.Identity, size)
	for i, n := range members {
		swarm[i] = n.nodeID
	}
	for i, n := range members {
		n.user.swarm = swarm
		// note: every sibling answers the ask with the contracts it hosts.
		n.user.fetch = func(_ *astral.Context, issuer *astral.Identity, yield func(*astral.Identity, *auth.SignedContract) bool) {
			for j, sc := range contracts {
				if j != i && sc.Issuer.IsEqual(issuer) && !yield(members[j].nodeID, sc) {
					return
				}
			}
		}
		for j, host := range members {
			if j != i {
				n.link(t, host.nodeID, routers[j])
			}
		}
	}

	// the slowed pushes: each host's contract reaches each sibling after the race
	var pushes sync.WaitGroup
	for i := range members {
		for j, n := range members {
			if j == i {
				continue
			}
			pushes.Add(1)
			go func() {
				defer pushes.Done()
				time.Sleep(relayPushDelay)
				n.index.IndexContract(networkCtx(), contracts[i])
			}()
		}
	}

	type pairResult struct {
		routeResult
		consumer, provider int
	}
	results := make(chan pairResult, size*(size-1))
	var calls sync.WaitGroup
	for c := range members {
		for p := range members {
			if p == c {
				continue
			}
			calls.Add(1)
			go func() {
				defer calls.Done()
				results <- pairResult{routeResult: members[c].routeFromNode(apps[p]), consumer: c, provider: p}
			}()
		}
	}
	calls.Wait()
	close(results)
	pushes.Wait()

	answered := 0
	for r := range results {
		switch {
		case r.err != nil:
			t.Errorf("node%d → app%d answered %v; want the provider's answer", r.consumer+1, r.provider+1, r.err)
		case r.answer != "echo":
			t.Errorf("node%d → app%d read %q; want %q", r.consumer+1, r.provider+1, r.answer, "echo")
		case r.took >= 1500*time.Millisecond:
			t.Errorf("node%d → app%d took %v; want under 1.5 s", r.consumer+1, r.provider+1, r.took)
		default:
			answered++
		}
	}
	if answered != size*(size-1) {
		t.Fatalf("%d of %d pairs answered on the first call", answered, size*(size-1))
	}

	for p, router := range routers {
		routed := router.received()
		if len(routed) != size-1 {
			t.Fatalf("host node%d routed %d queries; want one from each remote consumer", p+1, len(routed))
		}
		for _, q := range routed {
			if !q.target.IsEqual(apps[p]) || q.caller.IsEqual(members[p].nodeID) {
				t.Fatalf("host node%d routed %v → %v; want a remote consumer's query to its app", p+1, q.caller, q.target)
			}
		}
	}
	for c, n := range members {
		if got := n.user.fetches.Load(); got != size-1 {
			t.Fatalf("node%d asked its siblings %d times; want one ask per remote provider (%d)", c+1, got, size-1)
		}
	}
}
