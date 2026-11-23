package nodes

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	exonetapi "github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/exonet"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// swapResolver yields the rows set last, for every identity.
type swapResolver struct {
	mu   sync.Mutex
	rows []*nodes.EndpointWithTTL
}

func (r *swapResolver) set(rows ...*nodes.EndpointWithTTL) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = rows
}

func (r *swapResolver) ResolveEndpoints(*astral.Context, *astral.Identity) (<-chan *nodes.EndpointWithTTL, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return sig.ArrayToChan(r.rows), nil
}

// addrDials records every dial by address and signals each one on dialed.
type addrDials struct {
	mu     sync.Mutex
	counts map[string]int
	dialed chan string
}

func newAddrDials() *addrDials {
	return &addrDials{counts: map[string]int{}, dialed: make(chan string, 16)}
}

func (l *addrDials) record(addr string) {
	l.mu.Lock()
	l.counts[addr]++
	l.mu.Unlock()
	l.dialed <- addr
}

func (l *addrDials) count(addr string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.counts[addr]
}

// waitDial fails the test unless addr is dialed within limit.
func (l *addrDials) waitDial(t *testing.T, addr string, limit time.Duration) {
	t.Helper()
	deadline := time.After(limit)
	for {
		select {
		case got := <-l.dialed:
			if got == addr {
				return
			}
		case <-deadline:
			t.Fatalf("%v not dialed within %v", addr, limit)
		}
	}
}

// A row stored while the round's first row hangs is dialed at once, and the hung dial keeps running.
func TestBasicRoundDialsRowsAddedMidRound(t *testing.T) {
	const stale, fresh = "10.77.0.11:1791", "10.78.0.11:1791"
	dials := newAddrDials()
	staleReturned := make(chan struct{})
	s := newBasicTestStrategy(t, func(ctx *astral.Context, ep exonetapi.Endpoint) (exonet.Conn, error) {
		dials.record(ep.Address())
		if ep.Address() == stale {
			// note: a dial to a gone address, which only the round's end stops
			<-ctx.Done()
			close(staleReturned)
			return nil, ctx.Err()
		}
		return nil, errors.New("refused")
	})
	resolver := &swapResolver{}
	resolver.set(nodes.NewEndpointWithTTL(mustTCP(t, stale)))
	s.mod.resolvers.Add(resolver)

	done, release := s.Join(astral.NewContext(context.Background()))
	defer release()
	dials.waitDial(t, stale, 5*time.Second)

	resolver.set(nodes.NewEndpointWithTTL(mustTCP(t, stale)), nodes.NewEndpointWithTTL(mustTCP(t, fresh)))
	s.EndpointsAdded()
	dials.waitDial(t, fresh, 2*time.Second)

	select {
	case <-staleReturned:
		t.Fatal("the stale dial returned before the round ended; want it kept running")
	default:
	}
	if n := dials.count(stale); n != 1 {
		t.Fatalf("stale row dialed %v times; want once", n)
	}

	release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("round did not finish after its last caller left")
	}
}

// countingResolver yields fixed rows and counts the resolves.
type countingResolver struct {
	rows     []*nodes.EndpointWithTTL
	resolves atomic.Int32
}

func (r *countingResolver) ResolveEndpoints(*astral.Context, *astral.Identity) (<-chan *nodes.EndpointWithTTL, error) {
	r.resolves.Add(1)
	return sig.ArrayToChan(r.rows), nil
}

// A signal sent before a round starts makes the round resolve no second time.
func TestBasicRoundDropsSignalFromBeforeIt(t *testing.T) {
	const row = "10.77.0.11:1791"
	dials := newAddrDials()
	s := newBasicTestStrategy(t, func(_ *astral.Context, ep exonetapi.Endpoint) (exonet.Conn, error) {
		dials.record(ep.Address())
		return nil, errors.New("refused")
	})
	resolver := &countingResolver{rows: []*nodes.EndpointWithTTL{nodes.NewEndpointWithTTL(mustTCP(t, row))}}
	s.mod.resolvers.Add(resolver)

	s.EndpointsAdded()
	done, release := s.Join(astral.NewContext(context.Background()))
	defer release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("basic round did not finish")
	}
	if n := resolver.resolves.Load(); n != 1 {
		t.Fatalf("round resolved %v times; want once", n)
	}
	if n := dials.count(row); n != 1 {
		t.Fatalf("row dialed %v times; want once", n)
	}
}

// fakeRowsStrategy counts the EndpointsAdded signals it gets.
type fakeRowsStrategy struct {
	mu    sync.Mutex
	added int
}

func (f *fakeRowsStrategy) Name() string { return "fake" }
func (f *fakeRowsStrategy) Join(*astral.Context) (<-chan struct{}, func()) {
	done := make(chan struct{})
	close(done)
	return done, func() {}
}
func (f *fakeRowsStrategy) EndpointsAdded() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added++
}

var _ nodesmod.LinkStrategy = &fakeRowsStrategy{}

// The pool passes the news of new rows to the target's linker, and creates no linker for it.
func TestLinkPoolPassesEndpointsAddedToLinker(t *testing.T) {
	pool := &LinkPool{}
	target, other := newTestIdentity(t), newTestIdentity(t)
	fake := &fakeRowsStrategy{}
	linker := &NodeLinker{Target: target}
	linker.strategies.Set("fake", fake)
	pool.linkers.Set(target.String(), linker)

	pool.endpointsAdded(target)
	pool.endpointsAdded(other)

	if fake.added != 1 {
		t.Fatalf("strategy got %v signals; want 1", fake.added)
	}
	if _, ok := pool.linkers.Get(other.String()); ok {
		t.Fatal("endpointsAdded created a linker for a target with no round")
	}
}
