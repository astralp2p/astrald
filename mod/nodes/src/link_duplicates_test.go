package nodes

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/events"
	exonetapi "github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tor"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/exonet"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// settleTimeout bounds how long a test waits for both ends to agree on their links.
const settleTimeout = 3 * time.Second

// hookEvents records events and runs onCreated, when set, for each LinkCreatedEvent.
type hookEvents struct {
	recordingEvents
	onCreated func()
}

func (e *hookEvents) Emit(data astral.Object) *events.Event {
	if _, ok := data.(*nodes.LinkCreatedEvent); ok && e.onCreated != nil {
		e.onCreated()
	}
	return e.recordingEvents.Emit(data)
}

// linkEnd is one node of a simulated pair: a module with its own key, pool and events.
type linkEnd struct {
	mod    *Module
	id     *astral.Identity
	events *hookEvents
}

func newLinkEnd(t *testing.T) *linkEnd {
	t.Helper()
	key := newNegotiationKey(t)
	id := astral.IdentityFromPubKey(key.PubKey())
	ev := &hookEvents{}
	mod := &Module{
		Deps:       Deps{Events: ev},
		privateKey: &crypto.PrivateKey{Key: key.Serialize()},
		node:       &identityNode{id: id},
		log:        log.New(id),
		ctx:        astral.NewContext(context.Background()),
	}
	mod.linkPool = NewLinkPool(mod)
	t.Cleanup(mod.linkPool.Close)
	return &linkEnd{mod: mod, id: id, events: ev}
}

// newLinkPair returns two ends ordered by identity: lower first.
func newLinkPair(t *testing.T) (lower, higher *linkEnd) {
	t.Helper()
	a, b := newLinkEnd(t), newLinkEnd(t)
	if bytes.Compare(identityKey(a.id), identityKey(b.id)) > 0 {
		a, b = b, a
	}
	return a, b
}

// pipeTo returns the dialer's end of a pipe whose other end to accepts as an inbound link.
func (to *linkEnd) pipeTo(t *testing.T) exonet.Conn {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() {
		_ = a.Close()
		_ = b.Close()
	})
	go func() { _ = to.mod.EstablishInboundLink(context.Background(), &negotiationPipeConn{Conn: b}) }()
	return &negotiationPipeConn{Conn: a, outbound: true}
}

// dial links from to `to` and returns the dialer's link, already registered in both pools.
func (from *linkEnd) dial(t *testing.T, to *linkEnd) *Link {
	t.Helper()
	raw, err := from.mod.EstablishOutboundLink(context.Background(), to.id, to.pipeTo(t))
	if err != nil {
		t.Fatalf("EstablishOutboundLink: %v", err)
	}
	link := raw.(*Link)
	waitFor(t, "the inbound end to register the link", func() bool { return to.find(link.id) != nil })
	return link
}

// strategyLink dials to the way a strategy does and offers the link to the dialer's pool.
func (from *linkEnd) strategyLink(t *testing.T, to *linkEnd) *Link {
	t.Helper()
	link := from.dial(t, to)
	from.mod.linkPool.offerStrategyLink(link, nodesmod.StrategyBasic)
	return link
}

// find returns the pooled link with id, or nil.
func (e *linkEnd) find(id astral.Nonce) *Link {
	for _, l := range e.mod.linkPool.links.Clone() {
		if l.id == id {
			return l
		}
	}
	return nil
}

// liveIDs returns the ids of the live pooled links to peer.
func (e *linkEnd) liveIDs(peer *linkEnd) []astral.Nonce {
	var ids []astral.Nonce
	for _, l := range e.mod.linkPool.links.Clone() {
		if l.Err() == nil && l.RemoteIdentity().IsEqual(peer.id) {
			ids = append(ids, l.id)
		}
	}
	return ids
}

// closedEvents counts the LinkClosedEvents this end emitted so far.
func (e *linkEnd) closedEvents() int {
	n := 0
	for _, ev := range e.events.recorded() {
		if _, ok := ev.(*nodes.LinkClosedEvent); ok {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(settleTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %v", settleTimeout, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// requireOneSharedLink fails unless both ends settle on exactly one live link to each other, the same link, and keep it.
func requireOneSharedLink(t *testing.T, a, b *linkEnd) astral.Nonce {
	t.Helper()
	agree := func() bool {
		ia, ib := a.liveIDs(b), b.liveIDs(a)
		return len(ia) == 1 && len(ib) == 1 && ia[0] == ib[0]
	}
	deadline := time.Now().Add(settleTimeout)
	for !agree() {
		if time.Now().After(deadline) {
			t.Fatalf("ends hold live links %v and %v; want one shared link", a.liveIDs(b), b.liveIDs(a))
		}
		time.Sleep(10 * time.Millisecond)
	}
	// why: a late close would leave no link; the agreement must hold.
	time.Sleep(200 * time.Millisecond)
	if !agree() {
		t.Fatalf("after settling, ends hold live links %v and %v; want one shared link", a.liveIDs(b), b.liveIDs(a))
	}
	return a.liveIDs(b)[0]
}

// stubDialTo makes e's exonet dial peer over a pipe for every endpoint, and resolve one tcp and one tor row for every identity.
func (e *linkEnd) stubDialTo(t *testing.T, peer *linkEnd) {
	t.Helper()
	e.mod.Exonet = &stubExonet{dial: func(*astral.Context, exonetapi.Endpoint) (exonet.Conn, error) {
		return peer.pipeTo(t), nil
	}}
	e.mod.resolvers.Add(&stubResolver{rows: []*nodes.EndpointWithTTL{
		nodes.NewEndpointWithTTL(mustTCP(t, "127.0.0.1:1791")),
		nodes.NewEndpointWithTTL(&tor.Endpoint{}),
	}})
}

// TestStrategyLinkWithoutWatcherStays: AC1. A basic round that nobody waits
// for, to a peer with no other link, leaves its link in both pools, open.
func TestStrategyLinkWithoutWatcherStays(t *testing.T) {
	for _, byLower := range []bool{true, false} {
		t.Run(map[bool]string{true: "lower dials", false: "higher dials"}[byLower], func(t *testing.T) {
			from, to := newLinkPair(t)
			if !byLower {
				from, to = to, from
			}
			from.stubDialTo(t, to)
			s := (&BasicLinkStrategyFactory{mod: from.mod, networks: basicNetworks}).Build(to.id)
			s.Signal(astral.NewContext(context.Background()))
			select {
			case <-s.Done():
			case <-time.After(settleTimeout):
				t.Fatal("basic round did not finish")
			}

			requireOneSharedLink(t, from, to)
			if n := from.closedEvents(); n != 0 {
				t.Fatalf("dialer closed %v links; want none for the only link to its peer", n)
			}
		})
	}
}

// TestDuplicateStrategyLinkPrunedBySameRuleOnBothEnds: AC2. With a live link
// between the pair, a second strategy link leaves exactly one link, the same
// on both ends: the link the lower identity dialed, else the older link.
func TestDuplicateStrategyLinkPrunedBySameRuleOnBothEnds(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second bool // true: the lower identity dials
		keepFirst     bool
	}{
		{"lower then lower", true, true, true},
		{"lower then higher", true, false, true},
		{"higher then lower", false, true, false},
		{"higher then higher", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lower, higher := newLinkPair(t)
			dial := func(byLower bool) *Link {
				if byLower {
					return lower.strategyLink(t, higher)
				}
				return higher.strategyLink(t, lower)
			}
			first := dial(tc.first)
			second := dial(tc.second)

			want := second.id
			if tc.keepFirst {
				want = first.id
			}
			if got := requireOneSharedLink(t, lower, higher); got != want {
				t.Fatalf("both ends kept link %v; want %v", got, want)
			}
			loser := first
			if tc.keepFirst {
				loser = second
			}
			if !errors.Is(loser.Err(), nodesmod.ErrExcessLink) {
				t.Fatalf("losing link closed with %v; want %v", loser.Err(), nodesmod.ErrExcessLink)
			}
		})
	}
}

// TestForcedRequestKeepsOutrankedLink: a link a caller asked for as new is
// not pruned, even when an existing link outranks it.
func TestForcedRequestKeepsOutrankedLink(t *testing.T) {
	lower, higher := newLinkPair(t)
	lower.strategyLink(t, higher)

	w := higher.mod.linkPool.subscribe(func(*Link, *string) bool { return true }, true)
	defer higher.mod.linkPool.unsubscribe(w)
	forced := higher.strategyLink(t, lower)

	select {
	case got := <-w.ch:
		if got != forced {
			t.Fatalf("forced watcher got link %v; want the new link %v", got.id, forced.id)
		}
	default:
		t.Fatal("forced watcher got no link")
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(higher.liveIDs(lower)); n != 2 || forced.Err() != nil {
		t.Fatalf("higher holds %v live links, forced link error %v; want 2 and none", n, forced.Err())
	}
}

// TestTorDeliversBeforeDone: AC3. A RetrieveLink waiting on the tor strategy
// gets the link: the strategy is not Done while its link is undelivered.
func TestTorDeliversBeforeDone(t *testing.T) {
	a, b := newLinkPair(t)
	a.stubDialTo(t, b)
	a.mod.strategyFactories.Set(nodesmod.StrategyTor, &TorLinkStrategyFactory{
		mod:     a.mod,
		network: "tor",
		config: TorLinkStrategyConfig{
			QuickRetries:      1,
			Retries:           1,
			SignalTimeout:     settleTimeout,
			BackgroundTimeout: time.Second,
		},
	})

	// note: holding the pool lock once the link is registered stalls its delivery.
	var once sync.Once
	stalled := make(chan struct{})
	a.events.onCreated = func() {
		once.Do(func() {
			a.mod.linkPool.mu.Lock()
			close(stalled)
		})
	}

	ctx := astral.NewContext(context.Background())
	result := a.mod.linkPool.RetrieveLink(ctx, b.id, WithStrategies(nodesmod.StrategyTor))

	select {
	case <-stalled:
	case <-time.After(settleTimeout):
		t.Fatal("tor strategy produced no link")
	}
	select {
	case r := <-result:
		a.mod.linkPool.mu.Unlock()
		t.Fatalf("RetrieveLink answered (link %v, err %v) before the tor link was delivered", r.Link != nil, r.Err)
	case <-time.After(300 * time.Millisecond):
	}
	a.mod.linkPool.mu.Unlock()

	select {
	case r := <-result:
		if r.Err != nil || r.Link == nil {
			t.Fatalf("RetrieveLink answered err %v; want the tor link", r.Err)
		}
	case <-time.After(settleTimeout):
		t.Fatal("RetrieveLink did not answer")
	}
}

// TestSimultaneousDialsKeepOneLink: AC4. Both ends dial each other at once;
// both end with exactly one live link, the one the lower identity dialed.
func TestSimultaneousDialsKeepOneLink(t *testing.T) {
	t.Run("lower link completes first", func(t *testing.T) {
		lower, higher := newLinkPair(t)
		kept := lower.dial(t, higher)
		pruned := higher.dial(t, lower)
		lower.mod.linkPool.offerStrategyLink(kept, nodesmod.StrategyBasic)
		higher.mod.linkPool.offerStrategyLink(pruned, nodesmod.StrategyBasic)
		if got := requireOneSharedLink(t, lower, higher); got != kept.id {
			t.Fatalf("both ends kept %v; want the lower identity's link %v", got, kept.id)
		}
	})
	t.Run("higher link settles first", func(t *testing.T) {
		lower, higher := newLinkPair(t)
		pruned := higher.strategyLink(t, lower)
		kept := lower.strategyLink(t, higher)
		if got := requireOneSharedLink(t, lower, higher); got != kept.id {
			t.Fatalf("both ends kept %v; want the lower identity's link %v", got, kept.id)
		}
		if !errors.Is(pruned.Err(), nodesmod.ErrExcessLink) {
			t.Fatalf("higher identity's link closed with %v; want %v", pruned.Err(), nodesmod.ErrExcessLink)
		}
	})
	t.Run("concurrent basic rounds", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			lower, higher := newLinkPair(t)
			lower.stubDialTo(t, higher)
			higher.stubDialTo(t, lower)
			for _, e := range []*linkEnd{lower, higher} {
				e.mod.strategyFactories.Set(nodesmod.StrategyBasic, &BasicLinkStrategyFactory{mod: e.mod, networks: basicNetworks})
			}

			ctx := astral.NewContext(context.Background())
			rl := lower.mod.linkPool.RetrieveLink(ctx, higher.id, WithStrategies(nodesmod.StrategyBasic))
			rh := higher.mod.linkPool.RetrieveLink(ctx, lower.id, WithStrategies(nodesmod.StrategyBasic))
			for _, r := range []LinkResult{<-rl, <-rh} {
				if r.Err != nil {
					t.Fatalf("round %v: RetrieveLink: %v", i, r.Err)
				}
			}
			requireOneSharedLink(t, lower, higher)
		}
	})
}
