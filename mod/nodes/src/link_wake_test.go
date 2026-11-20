package nodes

import (
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

// testWakeDelay is the wake wait of the fixtures in this file.
const testWakeDelay = 300 * time.Millisecond

// boundConn is a pipe conn to the node remote whose transport reports the
// local endpoint local and sets the keepalive k.
type boundConn struct {
	keepaliveConn
	remote *astral.Identity
	local  exonet.Endpoint
}

func (c boundConn) RemoteIdentity() *astral.Identity       { return c.remote }
func (c boundConn) Outbound() bool                         { return true }
func (c boundConn) LocalEndpoint() exonet.Endpoint         { return c.local }
func (c boundConn) RemoteEndpoint() exonet.Endpoint        { return nil }
func (c boundConn) LinkKeepalive() exonetmod.LinkKeepalive { return c.k }

// wakeFixture is a pool on a module that records its events and wakes links by removed local IP.
type wakeFixture struct {
	*probeFixture
}

func newWakeFixture(t *testing.T) *wakeFixture {
	t.Helper()

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	localID := astral.GenerateIdentity()
	events := &recordingEvents{}
	mod := &Module{
		Deps:          Deps{Events: events, User: &countingUser{}},
		node:          &identityNode{id: localID},
		log:           log.New(localID),
		ctx:           ctx,
		linkWakeDelay: testWakeDelay,
		// note: a link event schedules an announcement; the wait outlasts these tests, which never expect one.
		announceDelay: time.Hour,
	}
	mod.linkPool = NewLinkPool(mod)

	return &wakeFixture{&probeFixture{mod: mod, events: events}}
}

// addLink adds a link to a new peer whose local endpoint is local, that never
// pings on its own and has the ping timeout given, and starts its peer.
func (f *wakeFixture) addLink(t *testing.T, local exonet.Endpoint, timeout time.Duration, answer bool) (*Link, *probePeer) {
	t.Helper()

	pipe, remote := net.Pipe()
	t.Cleanup(func() { pipe.Close(); remote.Close() })

	k := exonetmod.LinkKeepalive{Idle: time.Hour, Timeout: timeout}
	conn := boundConn{keepaliveConn{pipeIdentityConn{pipe}, k}, astral.GenerateIdentity(), local}
	link := newLink(f.mod, conn, astral.NewNonce(), true)
	peer := startProbePeer(t, remote, answer)
	if err := f.mod.linkPool.AddLink(link); err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	return link, peer
}

// settle waits out the start jitter of the ping loops, after which a loop waits for a probe trigger.
func settle() { time.Sleep(pingJitter + 100*time.Millisecond) }

// addressesChanged delivers to the module an address change event of this node.
func (f *wakeFixture) addressesChanged(t *testing.T, removed, added []string) {
	t.Helper()

	toIPs := func(list []string) (out []ip.IP) {
		for _, a := range list {
			out = append(out, ip.IP(net.ParseIP(a)))
		}
		return out
	}

	e := &events.Event{
		ID:       astral.NewNonce(),
		SourceID: f.mod.node.Identity(),
		Data:     &ip.EventNetworkAddressChanged{Removed: toIPs(removed), Added: toIPs(added)},
	}
	if err := f.mod.ReceiveObject(&recordingDrop{sender: f.mod.node.Identity(), object: e}); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}
}

func kcpEndpoint(addr string) *kcp.Endpoint {
	return &kcp.Endpoint{IP: ip.IP(net.ParseIP(addr)), Port: 1792}
}

// requireProbes checks that peer received exactly n probes.
func requireProbes(t *testing.T, what string, peer *probePeer, n int) {
	t.Helper()
	if got := len(peer.received()); got != n {
		t.Fatalf("%s received %v probes; want %v", what, got, n)
	}
}

// TestWakeByLocalIP_MatchingLinkPinged_OtherNot (AC1): an address change that
// removes the local IP of a link makes it send exactly one ping after the wait,
// and a link bound to another IP sends none.
func TestWakeByLocalIP_MatchingLinkPinged_OtherNot(t *testing.T) {
	f := newWakeFixture(t)
	gone, gonePeer := f.addLink(t, tcpEndpoint("192.0.2.1"), time.Second, true)
	other, otherPeer := f.addLink(t, tcpEndpoint("192.0.2.2"), time.Second, true)
	settle()

	start := time.Now()
	f.addressesChanged(t, []string{"192.0.2.1"}, nil)

	eventually(t, "a probe on the link bound to the removed IP", func() bool { return len(gonePeer.received()) > 0 })
	if waited := gonePeer.received()[0].Sub(start); waited < testWakeDelay-50*time.Millisecond {
		t.Fatalf("the probe came %v after the change; want after the %v wait", waited, testWakeDelay)
	}
	time.Sleep(absenceWindow)

	requireProbes(t, "the link bound to the removed IP", gonePeer, 1)
	requireProbes(t, "the link bound to another IP", otherPeer, 0)
	if gone.Err() != nil || other.Err() != nil {
		t.Fatalf("a link closed: %v, %v", gone.Err(), other.Err())
	}
}

// TestWakeByLocalIP_KcpAndMappedAddresses: a kcp link matches like a tcp one,
// and the same address matches in its 4-byte and 16-byte forms.
func TestWakeByLocalIP_KcpAndMappedAddresses(t *testing.T) {
	f := newWakeFixture(t)
	_, kcpPeer := f.addLink(t, kcpEndpoint("192.0.2.1"), time.Second, true)
	_, tcpPeer := f.addLink(t, tcpEndpoint("192.0.2.1"), time.Second, true)
	settle()

	f.mod.scheduleLinkWake(&ip.EventNetworkAddressChanged{Removed: []ip.IP{ip.IP(net.ParseIP("192.0.2.1").To4())}})

	eventually(t, "a probe on the kcp link", func() bool { return len(kcpPeer.received()) > 0 })
	eventually(t, "a probe on the tcp link", func() bool { return len(tcpPeer.received()) > 0 })
	time.Sleep(absenceWindow)

	requireProbes(t, "the kcp link", kcpPeer, 1)
	requireProbes(t, "the tcp link", tcpPeer, 1)
}

// TestLinkWake_BurstCoalesces (AC2): five events within 100 ms that remove the
// same IP make one wake round, so each matching link sends one ping.
func TestLinkWake_BurstCoalesces(t *testing.T) {
	f := newWakeFixture(t)
	_, firstPeer := f.addLink(t, tcpEndpoint("192.0.2.1"), time.Second, true)
	_, secondPeer := f.addLink(t, kcpEndpoint("192.0.2.1"), time.Second, true)
	_, otherPeer := f.addLink(t, tcpEndpoint("192.0.2.2"), time.Second, true)
	settle()

	for range 5 {
		f.addressesChanged(t, []string{"192.0.2.1"}, nil)
		time.Sleep(20 * time.Millisecond)
	}

	eventually(t, "a probe on each matching link", func() bool {
		return len(firstPeer.received()) > 0 && len(secondPeer.received()) > 0
	})
	// why: a wake round per event would ping again after the answer; the window covers a second round.
	time.Sleep(testWakeDelay + absenceWindow)

	requireProbes(t, "the first matching link", firstPeer, 1)
	requireProbes(t, "the second matching link", secondPeer, 1)
	requireProbes(t, "the link bound to another IP", otherPeer, 0)
}

// TestLinkWake_RemovalsInOneWaitJoin: removals of different IPs within one wait
// wake the links of all of them.
func TestLinkWake_RemovalsInOneWaitJoin(t *testing.T) {
	f := newWakeFixture(t)
	_, firstPeer := f.addLink(t, tcpEndpoint("192.0.2.1"), time.Second, true)
	_, secondPeer := f.addLink(t, tcpEndpoint("192.0.2.2"), time.Second, true)
	_, otherPeer := f.addLink(t, tcpEndpoint("192.0.2.3"), time.Second, true)
	settle()

	f.addressesChanged(t, []string{"192.0.2.1"}, nil)
	time.Sleep(50 * time.Millisecond)
	f.addressesChanged(t, []string{"192.0.2.2"}, nil)

	eventually(t, "a probe on each link bound to a removed IP", func() bool {
		return len(firstPeer.received()) > 0 && len(secondPeer.received()) > 0
	})
	time.Sleep(absenceWindow)

	requireProbes(t, "the first link", firstPeer, 1)
	requireProbes(t, "the second link", secondPeer, 1)
	requireProbes(t, "the link bound to a kept IP", otherPeer, 0)
}

// TestLinkWake_DeadLinkCloses_EmitsLinkClosedEvent (AC3): a matching link whose
// peer never answers closes within the ping timeout plus the wait plus 500 ms,
// and the pool emits LinkClosedEvent for it.
func TestLinkWake_DeadLinkCloses_EmitsLinkClosedEvent(t *testing.T) {
	const timeout = 400 * time.Millisecond
	f := newWakeFixture(t)
	dead, _ := f.addLink(t, tcpEndpoint("192.0.2.1"), timeout, false)
	settle()

	start := time.Now()
	f.addressesChanged(t, []string{"192.0.2.1"}, nil)

	limit := timeout + testWakeDelay + 500*time.Millisecond
	select {
	case <-dead.Done():
	case <-time.After(limit + time.Second):
		t.Fatalf("the dead link was still open %v after the change", limit+time.Second)
	}
	if took := time.Since(start); took > limit {
		t.Fatalf("the dead link closed %v after the change; want at most %v", took, limit)
	}

	eventually(t, "a LinkClosedEvent for the dead link", func() bool { return len(f.closedEvents()) > 0 })
	if closed := f.closedEvents(); len(closed) != 1 || closed[0].LinkCount != 0 {
		t.Fatalf("LinkClosedEvents %v; want one with LinkCount 0", closed)
	}
}

// TestLinkWake_HealthyLinkStays (AC4): a matching link whose peer answers gets
// one ping and stays open, and no LinkClosedEvent follows.
func TestLinkWake_HealthyLinkStays(t *testing.T) {
	const timeout = 300 * time.Millisecond
	f := newWakeFixture(t)
	healthy, peer := f.addLink(t, tcpEndpoint("192.0.2.1"), timeout, true)
	settle()

	f.addressesChanged(t, []string{"192.0.2.1"}, nil)

	eventually(t, "a probe on the link", func() bool { return len(peer.received()) > 0 })
	time.Sleep(timeout + absenceWindow)

	if err := healthy.Err(); err != nil {
		t.Fatalf("the healthy link closed: %v", err)
	}
	requireProbes(t, "the healthy link", peer, 1)
	if closed := f.closedEvents(); len(closed) != 0 {
		t.Fatalf("LinkClosedEvents %v; want none", closed)
	}
}

// TestWakeByLocalIP_NilOrNonIPEndpointSkipped (AC5): a link whose local endpoint
// is absent, not an IP endpoint, a nil endpoint pointer, or a wildcard address
// is never woken, while a link on the removed IP in the same round is.
func TestWakeByLocalIP_NilOrNonIPEndpointSkipped(t *testing.T) {
	f := newWakeFixture(t)
	_, matchPeer := f.addLink(t, tcpEndpoint("192.0.2.1"), time.Second, true)

	cases := []struct {
		name  string
		local exonet.Endpoint
	}{
		{"no endpoint", nil},
		{"gateway endpoint", gateway.NewEndpoint(astral.GenerateIdentity(), astral.GenerateIdentity())},
		{"nil tcp endpoint", (*tcp.Endpoint)(nil)},
		{"zero tcp endpoint", &tcp.Endpoint{}},
		{"wildcard IPv4 tcp endpoint", tcpEndpoint("0.0.0.0")},
		{"wildcard IPv6 kcp endpoint", kcpEndpoint("::")},
	}
	peers := make([]*probePeer, len(cases))
	for i, tc := range cases {
		_, peers[i] = f.addLink(t, tc.local, time.Second, true)
	}
	settle()

	// why: the wildcard addresses are in the set, to show that equality alone never matches them.
	f.addressesChanged(t, []string{"192.0.2.1", "0.0.0.0", "::"}, nil)

	eventually(t, "a probe on the link bound to the removed IP", func() bool { return len(matchPeer.received()) > 0 })
	time.Sleep(absenceWindow)

	for i, tc := range cases {
		requireProbes(t, tc.name+" link", peers[i], 0)
	}
	requireProbes(t, "the matching link", matchPeer, 1)
}

// TestLinkWake_AddedOnlyNoWake (AC6): an event that only adds addresses, and one
// that changes nothing, wakes no link, even when an added address is a link's own.
func TestLinkWake_AddedOnlyNoWake(t *testing.T) {
	f := newWakeFixture(t)
	_, peer := f.addLink(t, tcpEndpoint("192.0.2.1"), time.Second, true)
	settle()

	f.addressesChanged(t, nil, []string{"192.0.2.1", "192.0.2.9"})
	f.addressesChanged(t, nil, nil)
	time.Sleep(testWakeDelay + absenceWindow)

	requireProbes(t, "the link", peer, 0)
	if f.mod.wakePending.Load() {
		t.Fatal("an event that removes nothing started a wake wait")
	}
}

// TestLocalIP: only a concrete tcp or kcp address is a local IP.
func TestLocalIP(t *testing.T) {
	cases := []struct {
		name string
		ep   exonet.Endpoint
		want string
	}{
		{"tcp", tcpEndpoint("192.0.2.1"), "192.0.2.1"},
		{"kcp", kcpEndpoint("2001:db8::1"), "2001:db8::1"},
		{"nil", nil, ""},
		{"nil tcp pointer", (*tcp.Endpoint)(nil), ""},
		{"nil kcp pointer", (*kcp.Endpoint)(nil), ""},
		{"empty address", &tcp.Endpoint{}, ""},
		{"wildcard", tcpEndpoint("::"), ""},
		{"gateway", gateway.NewEndpoint(astral.GenerateIdentity(), astral.GenerateIdentity()), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := localIP(tc.ep)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("localIP = %v; want none", got)
				}
				return
			}
			if !net.IP(got).Equal(net.ParseIP(tc.want)) {
				t.Fatalf("localIP = %v; want %v", got, tc.want)
			}
		})
	}

	if localIPIn(tcpEndpoint("192.0.2.1"), []ip.IP{ip.IP(net.ParseIP("192.0.2.1").To4())}) != true {
		t.Fatal("the 16-byte form of an address did not match its 4-byte form")
	}
	if localIPIn(tcpEndpoint("192.0.2.1"), nil) {
		t.Fatal("an empty set matched")
	}
}
