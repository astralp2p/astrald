package nodes

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/nodes"
)

// retrievalProbe replaces the module's link retrieval (Module.retrieve): it
// records each retrieval, in order, in events and answers it with err after
// hold, or when the retrieval's context ends. A retrieval is a "retrieve" event
// naming its target; the deadline of its context is kept.
type retrievalProbe struct {
	hold time.Duration
	err  error
	log  *eventLog

	mu        sync.Mutex
	targets   []*astral.Identity
	deadlines []time.Time
}

// eventLog is an ordered record of what a routed query did.
type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) add(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *eventLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

func (p *retrievalProbe) retrieve(ctx *astral.Context, target *astral.Identity) <-chan LinkResult {
	deadline, _ := ctx.Deadline()
	p.mu.Lock()
	p.targets = append(p.targets, target)
	p.deadlines = append(p.deadlines, deadline)
	p.mu.Unlock()
	if p.log != nil {
		p.log.add("retrieve")
	}

	err := p.err
	if err == nil {
		err = errors.New("no endpoint answered")
	}
	result := make(chan LinkResult, 1)
	go func() {
		select {
		case <-time.After(p.hold):
			result <- LinkResult{Err: err}
		case <-ctx.Done():
			result <- LinkResult{Err: ctx.Err()}
		}
	}()
	return result
}

// retrieved returns the identities a link was retrieved to.
func (p *retrievalProbe) retrieved() []*astral.Identity {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*astral.Identity(nil), p.targets...)
}

// retrievedFor reports whether a link was retrieved to id.
func (p *retrievalProbe) retrievedFor(id *astral.Identity) bool {
	for _, target := range p.retrieved() {
		if target.IsEqual(id) {
			return true
		}
	}
	return false
}

// probe installs a retrievalProbe on the fixture's module.
func (f *pullFixture) probe(hold time.Duration) *retrievalProbe {
	p := &retrievalProbe{hold: hold}
	f.mod.retrieve = p.retrieve
	return p
}

// routeVia routes one query from the fixture's node to target with relays in
// q.Extra, under ctx.
func (f *pullFixture) routeVia(ctx *astral.Context, target *astral.Identity, relays ...*astral.Identity) (r routeResult) {
	q := astral.Launch(&astral.Query{
		Nonce:       astral.NewNonce(),
		Caller:      f.nodeID,
		Target:      target,
		QueryString: "app.op",
	})
	if len(relays) > 0 {
		q.Extra.Set(nodes.ExtraRelayVia, relays)
	}

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

func (f *pullFixture) callerCtx() (*astral.Context, context.CancelFunc) {
	return networkCtx().WithIdentity(f.nodeID).WithTimeout(effectTimeout)
}

// relaysFirstBudget is how long a query through a linked relay may take: the
// retrieval of a link to the target takes far longer, so it must not run first.
const relaysFirstBudget = 200 * time.Millisecond

// An app target holds stale endpoints, so retrieving a link to it blocks for
// 60 s. q.Extra lists a linked relay: the query routes through the relay in
// under 200 ms and no link is retrieved to the app.
// Covers RC-C acceptance criterion 1.
func TestRouteQueryRelaysFirstSkipsStaleRetrieve(t *testing.T) {
	f := newRoutingFixture(t)
	app, relay := astral.GenerateIdentity(), astral.GenerateIdentity()
	if err := f.mod.db.AddEndpoint(app, "tor", "stale.onion:80", nil); err != nil {
		t.Fatalf("add the stale endpoint: %v", err)
	}
	router := &hostRouter{reply: "hello"}
	f.link(t, relay, router)
	retrieval := f.probe(60 * time.Second)

	ctx, cancel := f.callerCtx()
	defer cancel()
	r := f.routeVia(ctx, app, relay)

	if r.err != nil {
		t.Fatalf("the query answered %v; want it routed through the relay", r.err)
	}
	if r.answer != "hello" {
		t.Fatalf("caller read %q; want the app's answer", r.answer)
	}
	if r.took >= relaysFirstBudget {
		t.Fatalf("the query took %v; want under %v", r.took, relaysFirstBudget)
	}
	if routed := router.received(); len(routed) != 1 || !routed[0].target.IsEqual(app) {
		t.Fatalf("the relay routed %v; want one query to the app", routed)
	}
	if retrieval.retrievedFor(app) {
		t.Fatal("a link was retrieved to the app; want the relay tried first")
	}
	if n := f.user.fetches.Load(); n != 0 {
		t.Fatalf("asked the siblings %d times; want 0 (the index named the relay)", n)
	}
}

// An app target holds no endpoints, so retrieving a link to it fails at once.
// q.Extra lists a linked relay: the query reaches the relay in under 200 ms and
// no link is retrieved to the app.
// Covers RC-C acceptance criterion 2.
func TestRouteQueryRelaysFirstNoEndpoints(t *testing.T) {
	f := newRoutingFixture(t)
	app, relay := astral.GenerateIdentity(), astral.GenerateIdentity()
	router := &hostRouter{reply: "hello"}
	f.link(t, relay, router)
	retrieval := f.probe(0)

	ctx, cancel := f.callerCtx()
	defer cancel()
	r := f.routeVia(ctx, app, relay)

	if r.err != nil || r.answer != "hello" {
		t.Fatalf("the query answered %q, %v; want the app's answer through the relay", r.answer, r.err)
	}
	if r.took >= relaysFirstBudget {
		t.Fatalf("the query took %v; want under %v", r.took, relaysFirstBudget)
	}
	if len(router.received()) != 1 {
		t.Fatalf("the relay routed %d queries; want 1", len(router.received()))
	}
	if retrieval.retrievedFor(app) {
		t.Fatal("a link was retrieved to the app; want the relay tried first")
	}
}

// q.Extra lists no relay: a link is retrieved to the target first, then the
// linked siblings are asked for the target's relay contracts, then the relay
// they name routes the query.
// Covers RC-C acceptance criterion 3.
func TestRouteQueryEmptyExtraRetrievesThenPulls(t *testing.T) {
	f := newRoutingFixture(t)
	app, host := astral.GenerateIdentity(), astral.GenerateIdentity()
	f.user.swarm = []*astral.Identity{f.nodeID, host}
	router := &hostRouter{reply: "hello"}
	f.link(t, host, router)

	events := &eventLog{}
	retrieval := f.probe(0)
	retrieval.log = events
	answer := answering(host, relayContractOf(app, host))
	f.user.fetch = func(ctx *astral.Context, issuer *astral.Identity, yield func(*astral.Identity, *auth.SignedContract) bool) {
		events.add("pull")
		answer(ctx, issuer, yield)
	}

	ctx, cancel := f.callerCtx()
	defer cancel()
	r := f.routeVia(ctx, app)

	if r.err != nil || r.answer != "hello" {
		t.Fatalf("the query answered %q, %v; want the app's answer through the pulled relay", r.answer, r.err)
	}
	if got := events.all(); len(got) != 2 || got[0] != "retrieve" || got[1] != "pull" {
		t.Fatalf("the router did %v; want the retrieval, then the pull", got)
	}
	if got := retrieval.retrieved(); len(got) != 1 || !got[0].IsEqual(app) {
		t.Fatalf("a link was retrieved to %v; want the app once", got)
	}
}

// A link to the target exists and q.Extra lists a relay: the link routes the
// query; the relay, the retrieval and the siblings are untouched.
// Covers RC-C acceptance criterion 4.
func TestRouteQueryExistingLinkBeatsRelays(t *testing.T) {
	f := newRoutingFixture(t)
	app, relay := astral.GenerateIdentity(), astral.GenerateIdentity()
	direct, viaRelay := &hostRouter{reply: "direct"}, &hostRouter{reply: "relayed"}
	f.link(t, app, direct)
	f.link(t, relay, viaRelay)
	retrieval := f.probe(0)

	ctx, cancel := f.callerCtx()
	defer cancel()
	r := f.routeVia(ctx, app, relay)

	if r.err != nil || r.answer != "direct" {
		t.Fatalf("the query answered %q, %v; want the answer over the existing link", r.answer, r.err)
	}
	if n := len(viaRelay.received()); n != 0 {
		t.Fatalf("the relay routed %d queries; want 0", n)
	}
	if got := retrieval.retrieved(); len(got) != 0 {
		t.Fatalf("links were retrieved to %v; want none", got)
	}
	if n := f.user.fetches.Load(); n != 0 {
		t.Fatalf("asked the siblings %d times; want 0", n)
	}
}

// Every relay in q.Extra fails: the answer is the first refusal, else
// unavailable, else route not found, and no link is retrieved to the target
// afterwards.
// Covers RC-C acceptance criterion 5.
func TestRouteQueryAllRelaysFailAnswers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(t *testing.T, f *pullFixture, relays []*astral.Identity)
		assert func(t *testing.T, err error)
	}{
		{
			name: "a refusal outranks an unreachable relay",
			setup: func(t *testing.T, f *pullFixture, relays []*astral.Identity) {
				// relays[0] is not linked, and retrieval to it fails.
				f.link(t, relays[1], &failingRouter{err: rejectedWith(5)})
				f.link(t, relays[2], &failingRouter{err: rejectedWith(6)})
			},
			assert: func(t *testing.T, err error) { assertRejectedWith(t, err, 5) },
		},
		{
			name: "unreachable relays answer unavailable",
			setup: func(t *testing.T, f *pullFixture, relays []*astral.Identity) {
				f.link(t, relays[1], &failingRouter{err: routeNotFound()})
			},
			assert: func(t *testing.T, err error) { assertRejectedWith(t, err, astral.CodeUnavailable) },
		},
		{
			name: "relays with no route answer route not found",
			setup: func(t *testing.T, f *pullFixture, relays []*astral.Identity) {
				for _, relay := range relays {
					f.link(t, relay, &failingRouter{err: routeNotFound()})
				}
			},
			assert: func(t *testing.T, err error) { assertRouteNotFound(t, err) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRoutingFixture(t)
			app := astral.GenerateIdentity()
			relays := newRelays(3)
			tc.setup(t, f, relays)
			retrieval := f.probe(0)

			ctx, cancel := f.callerCtx()
			defer cancel()
			r := f.routeVia(ctx, app, relays...)

			tc.assert(t, r.err)
			if retrieval.retrievedFor(app) {
				t.Fatal("a link was retrieved to the app after the relays failed; want no fallback")
			}
			if n := f.user.fetches.Load(); n != 0 {
				t.Fatalf("asked the siblings %d times; want 0", n)
			}
		})
	}
}

// The caller's context ends while the query waits for a relay link: routing
// answers the context's error and tries no later relay.
// Covers RC-C acceptance criterion 5.
func TestRouteQueryRelaysFirstCancelledCallerReturnsTheContextError(t *testing.T) {
	f := newRoutingFixture(t)
	app := astral.GenerateIdentity()
	relays := newRelays(2)
	f.link(t, relays[1], &hostRouter{reply: "late"})
	retrieval := f.probe(time.Hour)

	ctx, cancelCaller := networkCtx().WithIdentity(f.nodeID).WithCancel()
	defer cancelCaller()
	go func() {
		for len(retrieval.retrieved()) == 0 {
			time.Sleep(time.Millisecond)
		}
		cancelCaller()
	}()

	r := f.routeVia(ctx, app, relays...)

	if !errors.Is(r.err, context.Canceled) {
		t.Fatalf("routing answered %v; want the context's error", r.err)
	}
	if got := retrieval.retrieved(); len(got) != 1 || !got[0].IsEqual(relays[0]) {
		t.Fatalf("links were retrieved to %v; want the first relay only", got)
	}
}

// On the relays-first path, linking to a relay runs under a reach context of
// its own: it ends at the caller's deadline when that is sooner than the reach
// budget, else the reach budget after it starts.
// Covers RC-C acceptance criterion 6.
func TestRouteQueryRelayReachCtxDeadline(t *testing.T) {
	t.Run("derived from the caller's deadline", func(t *testing.T) {
		f := newRoutingFixture(t)
		app, relay := astral.GenerateIdentity(), astral.GenerateIdentity()
		retrieval := f.probe(0)

		ctx, cancel := networkCtx().WithIdentity(f.nodeID).WithTimeout(7 * time.Second)
		defer cancel()
		callerDeadline, _ := ctx.Deadline()
		f.routeVia(ctx, app, relay)

		retrieval.mu.Lock()
		defer retrieval.mu.Unlock()
		if len(retrieval.deadlines) != 1 || !retrieval.targets[0].IsEqual(relay) {
			t.Fatalf("links were retrieved to %v; want the relay once", retrieval.targets)
		}
		if got := retrieval.deadlines[0]; got.IsZero() || !got.Equal(callerDeadline) {
			t.Fatalf("the reach context ends at %v; want the caller's deadline %v", got, callerDeadline)
		}
	})

	t.Run("the reach budget without a caller deadline", func(t *testing.T) {
		f := newRoutingFixture(t)
		app, relay := astral.GenerateIdentity(), astral.GenerateIdentity()
		retrieval := f.probe(0)

		ctx, cancel := networkCtx().WithIdentity(f.nodeID).WithCancel()
		defer cancel()
		start := time.Now()
		f.routeVia(ctx, app, relay)

		retrieval.mu.Lock()
		defer retrieval.mu.Unlock()
		if len(retrieval.deadlines) != 1 {
			t.Fatalf("%d links were retrieved; want 1", len(retrieval.deadlines))
		}
		got := retrieval.deadlines[0]
		if got.Before(start.Add(relayReachTimeout)) || got.After(time.Now().Add(relayReachTimeout)) {
			t.Fatalf("the reach context ends at %v; want %v after the query started", got, relayReachTimeout)
		}
	})

	t.Run("the target is never its own relay", func(t *testing.T) {
		f := newRoutingFixture(t)
		app := astral.GenerateIdentity()
		retrieval := f.probe(0)

		ctx, cancel := f.callerCtx()
		defer cancel()
		r := f.routeVia(ctx, app, app)

		assertRouteNotFound(t, r.err)
		if got := retrieval.retrieved(); len(got) != 0 {
			t.Fatalf("links were retrieved to %v; want none", got)
		}
	})
}
