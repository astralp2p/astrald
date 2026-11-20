package nodes

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	exonetapi "github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tor"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/exonet"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// gatedDial is a dialer that links to a peer only once its gate opens.
type gatedDial struct {
	gate    chan struct{}
	started atomic.Int32 // dials begun
	aborted atomic.Int32 // dials ended by their context before the gate opened
}

// gateDialTo makes e dial peer through a closed gate, and resolve one tcp and one tor row for every identity.
func (e *linkEnd) gateDialTo(t *testing.T, peer *linkEnd) *gatedDial {
	t.Helper()
	g := &gatedDial{gate: make(chan struct{})}
	e.mod.Exonet = &stubExonet{dial: func(ctx *astral.Context, _ exonetapi.Endpoint) (exonet.Conn, error) {
		g.started.Add(1)
		select {
		case <-g.gate:
			return peer.pipeTo(t), nil
		case <-ctx.Done():
			g.aborted.Add(1)
			return nil, ctx.Err()
		}
	}}
	e.mod.resolvers.Add(&stubResolver{rows: []*nodes.EndpointWithTTL{
		nodes.NewEndpointWithTTL(mustTCP(t, "127.0.0.1:1791")),
		nodes.NewEndpointWithTTL(&tor.Endpoint{}),
	}})
	return g
}

// natTestFactory builds NAT strategies whose traversal dials the target's tcp row
// through the module's exonet, standing in for the punch.
type natTestFactory struct {
	mod         *Module
	roundBudget time.Duration
	endpoint    exonetapi.Endpoint
}

func (f *natTestFactory) Build(target *astral.Identity) nodesmod.LinkStrategy {
	s := &NATLinkStrategy{mod: f.mod, log: f.mod.log.AppendTag("nat"), target: target, roundBudget: f.roundBudget}
	s.traverse = func(ctx *astral.Context) error {
		conn, err := s.mod.Exonet.Dial(ctx, f.endpoint)
		if err != nil {
			return err
		}
		raw, err := s.mod.EstablishOutboundLink(ctx, s.target, conn)
		if err != nil {
			conn.Close()
			return err
		}
		s.mod.linkPool.offerStrategyLink(raw.(*Link), s.Name())
		return nil
	}
	return s
}

// roundStrategies registers each strategy under test at e with round budget; Tor's
// background phase lasts bg.
func roundStrategies(t *testing.T, e *linkEnd, budget, bg time.Duration) map[string]nodesmod.StrategyFactory {
	t.Helper()
	return map[string]nodesmod.StrategyFactory{
		nodesmod.StrategyBasic: &BasicLinkStrategyFactory{mod: e.mod, networks: basicNetworks, roundBudget: budget},
		nodesmod.StrategyTor: &TorLinkStrategyFactory{mod: e.mod, network: "tor", config: TorLinkStrategyConfig{
			QuickRetries: 1, Retries: 2, SignalTimeout: budget, BackgroundTimeout: bg,
		}},
		nodesmod.StrategyNAT: &natTestFactory{mod: e.mod, roundBudget: budget, endpoint: mustTCP(t, "127.0.0.1:1791")},
	}
}

// keeperOf returns the round keeper of a strategy under test.
func keeperOf(t *testing.T, s nodesmod.LinkStrategy) *roundKeeper {
	t.Helper()
	switch s := s.(type) {
	case *BasicLinkStrategy:
		return &s.rounds
	case *TorLinkStrategy:
		return &s.rounds
	case *NATLinkStrategy:
		return &s.rounds
	}
	t.Fatalf("strategy %T has no round keeper", s)
	return nil
}

// runningRound returns the running round of e's strategy name toward target; nil when none runs.
func runningRound(t *testing.T, e *linkEnd, target *astral.Identity, name string) *linkRound {
	t.Helper()
	linker, ok := e.mod.linkPool.linkers.Get(target.String())
	if !ok {
		return nil
	}
	s, ok := linker.strategies.Get(name)
	if !ok {
		t.Fatalf("no %v strategy", name)
	}
	k := keeperOf(t, s)
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.round
}

// refsOf returns the number of callers joined to r.
func refsOf(r *linkRound) int {
	if r == nil {
		return 0
	}
	r.keeper.mu.Lock()
	defer r.keeper.mu.Unlock()
	return r.refs
}

// awaitResult fails unless result answers within settleTimeout.
func awaitResult(t *testing.T, what string, result <-chan LinkResult) LinkResult {
	t.Helper()
	select {
	case r := <-result:
		return r
	case <-time.After(settleTimeout):
		t.Fatalf("%v: RetrieveLink did not answer", what)
		return LinkResult{}
	}
}

// TestLaterCallerGetsLinkAfterFirstLeaves: AC1, AC4. Caller 1 starts a round and
// cancels while caller 2 waits. Caller 1 returns on its own cancel, the round
// keeps dialing, and caller 2 receives the link.
func TestLaterCallerGetsLinkAfterFirstLeaves(t *testing.T) {
	for _, name := range []string{nodesmod.StrategyBasic, nodesmod.StrategyTor, nodesmod.StrategyNAT} {
		t.Run(name, func(t *testing.T) {
			a, b := newLinkPair(t)
			g := a.gateDialTo(t, b)
			for n, f := range roundStrategies(t, a, 10*time.Second, time.Second) {
				a.mod.strategyFactories.Set(n, f)
			}

			ctx1, cancel1 := astral.NewContext(context.Background()).WithCancel()
			defer cancel1()
			r1 := a.mod.linkPool.RetrieveLink(ctx1, b.id, WithStrategies(name))
			waitFor(t, "caller 1's round to dial", func() bool { return g.started.Load() > 0 })

			r2 := a.mod.linkPool.RetrieveLink(astral.NewContext(context.Background()), b.id, WithStrategies(name))
			round := runningRound(t, a, b.id, name)
			waitFor(t, "caller 2 to join the round", func() bool { return refsOf(round) == 2 })

			cancel1()
			if r := awaitResult(t, "caller 1", r1); !errors.Is(r.Err, context.Canceled) {
				t.Fatalf("caller 1 got (link %v, err %v); want context.Canceled", r.Link != nil, r.Err)
			}
			waitFor(t, "caller 1 to leave the round", func() bool { return refsOf(round) == 1 })
			if err := round.ctx.Err(); err != nil || g.aborted.Load() != 0 {
				t.Fatalf("round ctx error %v, %v aborted dials after caller 1 left; want a live round", err, g.aborted.Load())
			}

			close(g.gate)
			r := awaitResult(t, "caller 2", r2)
			if r.Err != nil || r.Link == nil || !r.Link.RemoteIdentity().IsEqual(b.id) {
				t.Fatalf("caller 2 got (link %v, err %v); want the link to the peer", r.Link != nil, r.Err)
			}
		})
	}
}

// TestRoundCancelledWhenAllCallersLeave: AC2. Once every joined caller has
// left, the round's context is cancelled and its dial aborts.
func TestRoundCancelledWhenAllCallersLeave(t *testing.T) {
	for _, name := range []string{nodesmod.StrategyBasic, nodesmod.StrategyTor, nodesmod.StrategyNAT} {
		t.Run(name, func(t *testing.T) {
			a, b := newLinkPair(t)
			g := a.gateDialTo(t, b)
			for n, f := range roundStrategies(t, a, 10*time.Second, time.Second) {
				a.mod.strategyFactories.Set(n, f)
			}

			ctx1, cancel1 := astral.NewContext(context.Background()).WithCancel()
			defer cancel1()
			ctx2, cancel2 := astral.NewContext(context.Background()).WithCancel()
			defer cancel2()
			r1 := a.mod.linkPool.RetrieveLink(ctx1, b.id, WithStrategies(name))
			waitFor(t, "the round to dial", func() bool { return g.started.Load() > 0 })
			r2 := a.mod.linkPool.RetrieveLink(ctx2, b.id, WithStrategies(name))
			round := runningRound(t, a, b.id, name)
			waitFor(t, "caller 2 to join the round", func() bool { return refsOf(round) == 2 })

			cancel1()
			awaitResult(t, "caller 1", r1)
			cancel2()
			awaitResult(t, "caller 2", r2)

			select {
			case <-round.ctx.Done():
			case <-time.After(settleTimeout):
				t.Fatal("round ctx still live after every caller left")
			}
			if !errors.Is(round.ctx.Err(), context.Canceled) {
				t.Fatalf("round ctx error %v; want context.Canceled", round.ctx.Err())
			}
			waitFor(t, "the round's dial to abort", func() bool { return g.aborted.Load() > 0 })
		})
	}
}

// TestRoundBudgetEndsRound: AC3. A round that outlives its budget ends while
// callers are still joined; each caller gets ErrLinkNotProduced.
func TestRoundBudgetEndsRound(t *testing.T) {
	const budget = 200 * time.Millisecond
	for _, name := range []string{nodesmod.StrategyBasic, nodesmod.StrategyTor, nodesmod.StrategyNAT} {
		t.Run(name, func(t *testing.T) {
			a, b := newLinkPair(t)
			g := a.gateDialTo(t, b)
			for n, f := range roundStrategies(t, a, budget, budget) {
				a.mod.strategyFactories.Set(n, f)
			}

			start := time.Now()
			ctx := astral.NewContext(context.Background())
			r1 := a.mod.linkPool.RetrieveLink(ctx, b.id, WithStrategies(name))
			waitFor(t, "the round to dial", func() bool { return g.started.Load() > 0 })
			r2 := a.mod.linkPool.RetrieveLink(ctx, b.id, WithStrategies(name))

			for i, r := range []<-chan LinkResult{r1, r2} {
				got := awaitResult(t, "caller", r)
				if !errors.Is(got.Err, nodesmod.ErrLinkNotProduced) {
					t.Fatalf("caller %v got (link %v, err %v); want ErrLinkNotProduced", i+1, got.Link != nil, got.Err)
				}
			}
			if elapsed := time.Since(start); elapsed < budget {
				t.Fatalf("round ended after %v; want no earlier than its %v budget", elapsed, budget)
			}
		})
	}
}

// TestReleaseIsIdempotent: AC4. A second release by the same caller does not
// cancel a round another caller holds; a caller whose context ends leaves.
func TestReleaseIsIdempotent(t *testing.T) {
	a, b := newLinkPair(t)
	a.gateDialTo(t, b)
	s := (&BasicLinkStrategyFactory{mod: a.mod, networks: basicNetworks}).Build(b.id).(*BasicLinkStrategy)

	_, release1 := s.Join(astral.NewContext(context.Background()))
	ctx2, cancel2 := astral.NewContext(context.Background()).WithCancel()
	defer cancel2()
	_, release2 := s.Join(ctx2)
	defer release2()

	s.rounds.mu.Lock()
	round := s.rounds.round
	s.rounds.mu.Unlock()
	if refsOf(round) != 2 {
		t.Fatalf("round holds %v callers; want 2", refsOf(round))
	}

	release1()
	release1()
	if n := refsOf(round); n != 1 || round.ctx.Err() != nil {
		t.Fatalf("after a double release the round holds %v callers, ctx error %v; want 1 and a live round", n, round.ctx.Err())
	}

	cancel2()
	select {
	case <-round.ctx.Done():
	case <-time.After(settleTimeout):
		t.Fatal("round ctx still live after its last caller's context ended")
	}
	release2()
	if n := refsOf(round); n != 0 {
		t.Fatalf("round holds %v callers after release following context end; want 0", n)
	}
}

// TestTorBackgroundRunsAfterFirstCallerLeft: AC5. Caller 1 leaves during the
// foreground phase while caller 2 stays; the background phase still runs and
// links. A round every caller left runs no background phase.
func TestTorBackgroundRunsAfterFirstCallerLeft(t *testing.T) {
	const foreground = 500 * time.Millisecond
	for _, tc := range []struct {
		name       string
		stay       bool // caller 2 stays joined through the foreground phase
		wantLinked bool
	}{
		{"caller 2 stays", true, true},
		{"every caller left", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := newLinkPair(t)
			g := a.gateDialTo(t, b)
			a.mod.strategyFactories.Set(nodesmod.StrategyTor, roundStrategies(t, a, foreground, settleTimeout)[nodesmod.StrategyTor])

			ctx1, cancel1 := astral.NewContext(context.Background()).WithCancel()
			defer cancel1()
			ctx2, cancel2 := astral.NewContext(context.Background()).WithCancel()
			defer cancel2()
			r1 := a.mod.linkPool.RetrieveLink(ctx1, b.id, WithStrategies(nodesmod.StrategyTor))
			waitFor(t, "the foreground phase to dial", func() bool { return g.started.Load() > 0 })
			r2 := a.mod.linkPool.RetrieveLink(ctx2, b.id, WithStrategies(nodesmod.StrategyTor))
			round := runningRound(t, a, b.id, nodesmod.StrategyTor)
			waitFor(t, "caller 2 to join the round", func() bool { return refsOf(round) == 2 })

			cancel1()
			awaitResult(t, "caller 1", r1)
			if !tc.stay {
				cancel2()
			}
			awaitResult(t, "caller 2", r2)

			select {
			case <-round.done:
			case <-time.After(settleTimeout):
				t.Fatal("the foreground phase did not end")
			}
			dialsAtForegroundEnd := g.started.Load()
			close(g.gate)

			if tc.wantLinked {
				waitFor(t, "the background phase to link", func() bool { return len(a.liveIDs(b)) == 1 })
				return
			}
			time.Sleep(200 * time.Millisecond)
			if n := len(a.liveIDs(b)); n != 0 || g.started.Load() != dialsAtForegroundEnd {
				t.Fatalf("abandoned round: %v links, %v dials after the foreground phase; want none", n, g.started.Load()-dialsAtForegroundEnd)
			}
		})
	}
}
