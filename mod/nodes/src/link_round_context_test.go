package nodes

import (
	"context"
	"errors"
	"testing"
	"time"

	exonetapi "github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/exonet"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// TestJoinBeforeRunDoesNotPanic: a RetrieveLink on a loaded module that Run has
// not started reaches every strategy's Join with the module context Load set.
func TestJoinBeforeRunDoesNotPanic(t *testing.T) {
	id := newTestIdentity(t)
	mod := &Module{log: log.New(id)}
	mod.initContext(id)
	mod.linkPool = NewLinkPool(mod)
	mod.RegisterLinkStrategy(nodesmod.StrategyBasic, newBasicLinkStrategyFactory(mod))
	mod.RegisterLinkStrategy(nodesmod.StrategyTor, &TorLinkStrategyFactory{mod: mod, network: "tor", config: TorLinkStrategyConfig{
		QuickRetries: 1, Retries: 1, SignalTimeout: time.Second, BackgroundTimeout: time.Second,
	}})
	mod.RegisterLinkStrategy(nodesmod.StrategyNAT, &natTestFactory{mod: mod, roundBudget: time.Second, endpoint: mustTCP(t, "127.0.0.1:1791")})

	// why: the module has no resolver and no dialable endpoint, so every round ends without a link.
	mod.Exonet = &stubExonet{dial: func(*astral.Context, exonetapi.Endpoint) (exonet.Conn, error) {
		return nil, errors.New("no route")
	}}
	for _, name := range []string{nodesmod.StrategyBasic, nodesmod.StrategyTor, nodesmod.StrategyNAT} {
		t.Run(name, func(t *testing.T) {
			ctx := astral.NewContext(context.Background())
			r := awaitResult(t, name, mod.linkPool.RetrieveLink(ctx, newTestIdentity(t), WithStrategies(name)))
			if r.Link != nil || !errors.Is(r.Err, nodesmod.ErrLinkNotProduced) {
				t.Fatalf("got (link %v, err %v); want ErrLinkNotProduced", r.Link != nil, r.Err)
			}
		})
	}
}

// TestModuleContextEndsWithCancel: the module context is live from initContext
// and ends when the module cancels it, as Run does when its own context ends.
func TestModuleContextEndsWithCancel(t *testing.T) {
	id := newTestIdentity(t)
	mod := &Module{}
	mod.initContext(id)

	if mod.ctx == nil || mod.ctx.Err() != nil {
		t.Fatalf("module context %v, error %v after initContext; want live", mod.ctx, mod.ctx.Err())
	}
	if !mod.ctx.Identity().IsEqual(id) || mod.ctx.Zone()&astral.ZoneNetwork == 0 {
		t.Fatalf("module context identity %v zone %v; want %v with the network zone", mod.ctx.Identity(), mod.ctx.Zone(), id)
	}
	mod.cancel()
	if err := mod.ctx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("module context error %v after cancel; want context.Canceled", err)
	}
}

// TestJoinReturnsTheJoinedRoundsDone: a caller's done channel belongs to the round
// it joined, not to a newer round that replaced it.
func TestJoinReturnsTheJoinedRoundsDone(t *testing.T) {
	parent, stop := astral.NewContext(context.Background()).WithCancel()
	defer stop()

	var k roundKeeper
	hold := make(chan struct{})
	defer close(hold)
	run := func(*linkRound) { <-hold }

	doneA, releaseA := k.join(astral.NewContext(context.Background()), parent, time.Minute, run)
	defer releaseA()
	first := runningKeeperRound(&k)

	// why: the last release cancels round 1's context while its run goroutine
	// still holds, so the next join starts round 2.
	releaseA()
	doneB, releaseB := k.join(astral.NewContext(context.Background()), parent, time.Minute, run)
	defer releaseB()
	second := runningKeeperRound(&k)

	if first == second {
		t.Fatal("second join reused the cancelled round; want a new round")
	}
	if doneA != first.done || doneB != second.done {
		t.Fatal("join returned a done channel of another round; want the joined round's")
	}
	select {
	case <-doneA:
		t.Fatal("round 1 done closed while its run holds")
	default:
	}
}

func runningKeeperRound(k *roundKeeper) *linkRound {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.round
}

// fixedDoneStrategy is a LinkStrategy whose Join returns a fixed done channel.
type fixedDoneStrategy struct {
	done chan struct{}
	left chan struct{}
}

func (s *fixedDoneStrategy) Name() string { return "fixed" }

func (s *fixedDoneStrategy) Join(*astral.Context) (<-chan struct{}, func()) {
	return s.done, func() { close(s.left) }
}

// TestActivateWaitsOnTheJoinedDone: Activate's done channel closes with the done
// channel Join returned, and its release calls the strategy's release.
func TestActivateWaitsOnTheJoinedDone(t *testing.T) {
	s := &fixedDoneStrategy{done: make(chan struct{}), left: make(chan struct{})}
	linker := &NodeLinker{}
	linker.strategies.Set("fixed", s)

	done, release := linker.Activate(astral.NewContext(context.Background()), []string{"fixed"})
	select {
	case <-done:
		t.Fatal("Activate done closed before the joined round finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(s.done)
	select {
	case <-done:
	case <-time.After(settleTimeout):
		t.Fatal("Activate done did not close after the joined round finished")
	}

	release()
	select {
	case <-s.left:
	default:
		t.Fatal("release did not leave the strategy's round")
	}
}
