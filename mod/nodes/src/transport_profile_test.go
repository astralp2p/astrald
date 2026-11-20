package nodes

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	exonetapi "github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tor"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/exonet"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

// setProfile replaces the profile of network for the test.
func setProfile(t *testing.T, network string, p TransportProfile) {
	t.Helper()
	old, had := transportProfiles[network]
	transportProfiles[network] = p
	t.Cleanup(func() {
		if had {
			transportProfiles[network] = old
		} else {
			delete(transportProfiles, network)
		}
	})
}

// TestTransportProfilesComplete: every basic network and tor has a profile with positive keepalive timings.
func TestTransportProfilesComplete(t *testing.T) {
	for _, network := range append(append([]string{}, basicNetworks...), "tor") {
		p, ok := transportProfiles[network]
		if !ok {
			t.Fatalf("network %v has no transport profile", network)
		}
		if p.Rank <= 0 || p.IdlePing <= 0 || p.PingTimeout <= 0 {
			t.Fatalf("profile of %v is %+v; want a positive rank and keepalive timings", network, p)
		}
	}
}

// TestTransportProfilesOrdered: ranks are strictly tcp > kcp > gw > tor, and an unknown network ranks 0.
func TestTransportProfilesOrdered(t *testing.T) {
	order := []string{"tcp", "kcp", "gw", "tor"}
	for i := 1; i < len(order); i++ {
		hi, lo := rankOf(order[i-1]), rankOf(order[i])
		if hi <= lo {
			t.Fatalf("rank of %v is %v, rank of %v is %v; want %v above %v", order[i-1], hi, order[i], lo, order[i-1], order[i])
		}
	}
	for _, network := range []string{"unknown", "", "udp"} {
		if got := rankOf(network); got != 0 {
			t.Fatalf("rankOf(%q) = %v; want 0", network, got)
		}
	}
}

// TestTransportProfileValues pins the shipped values: master's timings, with kcp at its 10 s idle ping; only the NAT idle back-off changed.
func TestTransportProfileValues(t *testing.T) {
	idle := map[string]time.Duration{"tcp": 15 * time.Second, "kcp": 10 * time.Second, "gw": 15 * time.Second, "tor": 15 * time.Second}
	for network, p := range transportProfiles {
		if p.IdlePing != idle[network] || p.PingTimeout != 10*time.Second {
			t.Fatalf("profile of %v is %+v; want %v idle ping and 10 s ping timeout", network, p, idle[network])
		}
	}
	budgets := map[string]time.Duration{"tcp": 0, "kcp": 5 * time.Second, "gw": 20 * time.Second, "tor": 0}
	for network, want := range budgets {
		if got := transportProfiles[network].RowBudget; got != want {
			t.Fatalf("row budget of %v is %v; want %v", network, got, want)
		}
	}
	if fallbackProfile.Rank != 0 || fallbackProfile.IdlePing != 15*time.Second || fallbackProfile.PingTimeout != 10*time.Second {
		t.Fatalf("fallback profile is %+v; want rank 0, 15 s, 10 s", fallbackProfile)
	}
	if want := (NATKeepalive{Fresh: 5 * time.Second, FreshFor: time.Minute, Idle: 15 * time.Second}); natKeepalive != want {
		t.Fatalf("NAT keepalive is %+v; want %+v", natKeepalive, want)
	}
}

// TestTryEndpointRowBudgetFromTable: a dial row runs under the budget its network's profile sets (kcp 5 s, gw 20 s) and tcp runs under none.
func TestTryEndpointRowBudgetFromTable(t *testing.T) {
	var mu sync.Mutex
	left := map[string]time.Duration{}
	bounded := map[string]bool{}
	s := newBasicTestStrategy(t, func(ctx *astral.Context, ep exonetapi.Endpoint) (exonet.Conn, error) {
		d, ok := ctx.Deadline()
		mu.Lock()
		defer mu.Unlock()
		bounded[ep.Network()] = ok
		if ok {
			left[ep.Network()] = time.Until(d)
		}
		return nil, errors.New("refused")
	})
	s.rowBudgets = rowBudgetsOf(basicNetworks)

	ctx := astral.NewContext(context.Background())
	s.tryEndpoint(ctx, nodes.NewEndpointWithTTL(mustTCP(t, "127.0.0.1:1791")))
	s.tryEndpoint(ctx, nodes.NewEndpointWithTTL(mustKCP(t, "127.0.0.1:1792")))
	s.tryEndpoint(ctx, nodes.NewEndpointWithTTL(gateway.NewEndpoint(newTestIdentity(t), s.target)))

	if bounded["tcp"] {
		t.Fatalf("tcp dial ran under a deadline; want none")
	}
	for network, budget := range map[string]time.Duration{"kcp": 5 * time.Second, "gw": 20 * time.Second} {
		if !bounded[network] || left[network] > budget || left[network] < budget-time.Second {
			t.Fatalf("%v dial had %v left (bounded %v); want just under %v", network, left[network], bounded[network], budget)
		}
	}
}

// networkedConn is a conn that reports remote as its remote endpoint, as a transport's conn does.
type networkedConn struct {
	pipeIdentityConn
	remote exonetapi.Endpoint
}

func (c networkedConn) Outbound() bool                     { return true }
func (c networkedConn) LocalEndpoint() exonetapi.Endpoint  { return nil }
func (c networkedConn) RemoteEndpoint() exonetapi.Endpoint { return c.remote }

// TestKeepaliveOf: the conn's own values win; a value it leaves zero comes from the link's network profile; an unknown network uses the fallback profile.
func TestKeepaliveOf(t *testing.T) {
	setProfile(t, "kcp", TransportProfile{Rank: 40, IdlePing: 7 * time.Second, PingTimeout: 3 * time.Second})
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()
	plain := pipeIdentityConn{local}

	got := keepaliveOf("kcp", keepaliveConn{plain, exonetmod.LinkKeepalive{Idle: time.Second, Timeout: 2 * time.Second}})
	if got.Idle != time.Second || got.Timeout != 2*time.Second {
		t.Fatalf("conn values: got %+v; want 1s/2s", got)
	}
	got = keepaliveOf("kcp", keepaliveConn{plain, exonetmod.LinkKeepalive{Idle: time.Second}})
	if got.Idle != time.Second || got.Timeout != 3*time.Second {
		t.Fatalf("conn interval only: got %+v; want 1s/3s (kcp profile timeout)", got)
	}
	got = keepaliveOf("kcp", plain)
	if got.Idle != 7*time.Second || got.Timeout != 3*time.Second {
		t.Fatalf("no conn values: got %+v; want the kcp profile 7s/3s", got)
	}
	got = keepaliveOf("no-such-network", plain)
	if got.Idle != fallbackProfile.IdlePing || got.Timeout != fallbackProfile.PingTimeout {
		t.Fatalf("unknown network: got %+v; want the fallback profile", got)
	}
}

// TestNewLinkTakesKeepaliveFromNetworkProfile: newLink derives the network from the conn's endpoint and reads that profile.
func TestNewLinkTakesKeepaliveFromNetworkProfile(t *testing.T) {
	setProfile(t, "kcp", TransportProfile{Rank: 40, IdlePing: 7 * time.Second, PingTimeout: 3 * time.Second})
	local, remote := net.Pipe()
	defer remote.Close()
	link := newLink(&Module{}, networkedConn{pipeIdentityConn{local}, mustKCP(t, "127.0.0.1:1792")}, 1, true)
	defer link.Close()
	if link.Network() != "kcp" || link.idlePing != 7*time.Second || link.pingTimeout != 3*time.Second {
		t.Fatalf("link network %v timing %v/%v; want kcp 7s/3s", link.Network(), link.idlePing, link.pingTimeout)
	}
}

// fakeClock is a wakeClock that moves only when the test fires its next timer.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
}

type fakeWaiter struct {
	at time.Time
	ch chan time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.waiters = append(c.waiters, fakeWaiter{at: c.now.Add(d), ch: ch})
	return ch
}

// fire waits for a pending timer, moves the clock to it and fires it.
func (c *fakeClock) fire(t *testing.T) {
	t.Helper()
	waitFor(t, "the wake loop to wait on the clock", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.waiters) > 0
	})
	c.mu.Lock()
	w := c.waiters[0]
	c.waiters = c.waiters[1:]
	c.now = w.at
	c.mu.Unlock()
	w.ch <- w.at
}

// TestNATKeepaliveSchedule: on a fake clock an idle NAT link wakes every Fresh for FreshFor, then every Idle; no gap reaches 25 s; the loop ends when the link is done.
func TestNATKeepaliveSchedule(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	start := clock.now
	wakes := make(chan time.Time, 1)
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		natWakeLoop(clock, natKeepalive, func() { wakes <- clock.Now() }, done)
		close(finished)
	}()

	prev := start
	freshWakes := int(natKeepalive.FreshFor / natKeepalive.Fresh)
	const idleWakes = 40
	for i := 0; i < freshWakes+idleWakes; i++ {
		clock.fire(t)
		at := <-wakes
		gap := at.Sub(prev)
		want := natKeepalive.Fresh
		if at.Sub(start) > natKeepalive.FreshFor {
			want = natKeepalive.Idle
		}
		if gap != want {
			t.Fatalf("wake %d came %v after the previous one (%v after start); want %v", i+1, gap, at.Sub(start), want)
		}
		if gap >= 25*time.Second {
			t.Fatalf("wake %d gap %v; want under 25s", i+1, gap)
		}
		prev = at
	}

	close(done)
	select {
	case <-finished:
	case <-time.After(settleTimeout):
		t.Fatal("the wake loop kept running after the link was done")
	}
	select {
	case at := <-wakes:
		t.Fatalf("wake at %v after the link was done", at.Sub(start))
	default:
	}
}

// TestPressureDetectorOnlyForConfiguredProfiles: a tor link gets a detector built from TorLinkPressureConfig; tcp, kcp and gw links get none.
func TestPressureDetectorOnlyForConfiguredProfiles(t *testing.T) {
	peer := newLinkEnd(t)
	dialer := newLinkEnd(t)
	dialer.mod.Exonet = &stubExonet{dial: func(_ *astral.Context, ep exonetapi.Endpoint) (exonet.Conn, error) {
		return endpointConn{peer.pipeTo(t), ep}, nil
	}}
	ctx := astral.NewContext(context.Background())

	torStrategy := &TorLinkStrategy{
		mod:     dialer.mod,
		log:     log.New(dialer.id),
		network: "tor",
		target:  peer.id,
	}
	link := torStrategy.tryEndpoint(ctx, nodes.NewEndpointWithTTL(&tor.Endpoint{}))
	if link == nil {
		t.Fatal("tor tryEndpoint returned no link")
	}
	defer link.Close()
	det, ok := link.pressure.(*linkPressureDetector)
	if !ok || det.cfg != TorLinkPressureConfig {
		t.Fatalf("tor link pressure detector is %#v; want one built from TorLinkPressureConfig", link.pressure)
	}

	basic := &BasicLinkStrategy{mod: dialer.mod, networks: basicNetworks, rowBudgets: rowBudgetsOf(basicNetworks), target: peer.id}
	rows := map[string]*nodes.EndpointWithTTL{
		"tcp": nodes.NewEndpointWithTTL(mustTCP(t, "127.0.0.1:1791")),
		"kcp": nodes.NewEndpointWithTTL(mustKCP(t, "127.0.0.1:1792")),
		"gw":  nodes.NewEndpointWithTTL(gateway.NewEndpoint(newTestIdentity(t), peer.id)),
	}
	for network, row := range rows {
		l := basic.tryEndpoint(ctx, row)
		if l == nil {
			t.Fatalf("%v tryEndpoint returned no link", network)
		}
		if l.pressure != nil {
			t.Fatalf("%v link has a pressure detector; want none", network)
		}
		l.Close()
	}
}
