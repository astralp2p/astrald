package nodes

import (
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"github.com/astralp2p/astrald/mod/nodes/frames"
)

// keepaliveConn is a pipe conn whose transport sets the keepalive k.
type keepaliveConn struct {
	pipeIdentityConn
	k exonetmod.LinkKeepalive
}

func (c keepaliveConn) LinkKeepalive() exonetmod.LinkKeepalive { return c.k }

// probePeer is the far end of a pipe link: it records the probes it receives
// and answers each with a pong when answer is set.
type probePeer struct {
	ch     *channel.Channel
	answer bool
	mu     sync.Mutex
	probes []time.Time
	pongs  chan struct{} // one value per pong the peer receives
}

func startProbePeer(t *testing.T, remote net.Conn, answer bool) *probePeer {
	t.Helper()
	p := &probePeer{ch: channel.New(remote), answer: answer, pongs: make(chan struct{}, 16)}
	go p.run()
	return p
}

func (p *probePeer) run() {
	for {
		obj, err := p.ch.Receive()
		if err != nil {
			return
		}
		f, ok := obj.(*frames.Ping)
		if !ok {
			continue
		}
		if f.Pong {
			p.pongs <- struct{}{}
			continue
		}
		p.mu.Lock()
		p.probes = append(p.probes, time.Now())
		p.mu.Unlock()
		if p.answer {
			p.ch.Send(&frames.Ping{Nonce: f.Nonce, Pong: true})
		}
	}
}

func (p *probePeer) received() []time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]time.Time(nil), p.probes...)
}

// newKeepaliveLink starts a link over a pipe whose transport sets idle and timeout.
func newKeepaliveLink(t *testing.T, mod *Module, idle, timeout time.Duration) (*Link, net.Conn) {
	t.Helper()
	local, remote := net.Pipe()
	t.Cleanup(func() { local.Close(); remote.Close() })
	conn := keepaliveConn{pipeIdentityConn{local}, exonetmod.LinkKeepalive{Idle: idle, Timeout: timeout}}
	return newLink(mod, conn, astral.NewNonce(), false), remote
}

// newIdleLink builds a link by literal, with no loops running, last hearing from its peer ago.
func newIdleLink(idle, ago time.Duration) *Link {
	s := &Link{idlePing: idle, done: make(chan struct{}), wakeCh: make(chan struct{}, 1)}
	s.lastRecv.Store(time.Now().Add(-ago).UnixNano())
	return s
}

// waitProbeAsync runs waitProbe and reports when it returned.
func waitProbeAsync(s *Link) <-chan time.Time {
	due := make(chan time.Time, 1)
	go func() {
		t := time.NewTimer(time.Hour)
		defer t.Stop()
		if s.waitProbe(t) {
			due <- time.Now()
		}
	}()
	return due
}

// TestIdleProbeWaitsForSilence: a link that heard from its peer 1 s ago, with
// an idle interval of 2 s, is not due a probe at the tick; one that heard
// nothing for the interval plus 1 s is due one at once.
func TestIdleProbeWaitsForSilence(t *testing.T) {
	recent := newIdleLink(2*time.Second, time.Second)
	defer close(recent.done)
	start := time.Now()
	due := waitProbeAsync(recent)
	select {
	case <-due:
		t.Fatal("a link that heard from its peer 1 s ago probed before its idle interval passed")
	case <-time.After(500 * time.Millisecond):
	}
	select {
	case at := <-due:
		if waited := at.Sub(start); waited < 900*time.Millisecond || waited > 1500*time.Millisecond {
			t.Fatalf("probe came after %v; want about 1 s, the rest of the idle interval", waited)
		}
	case <-time.After(effectTimeout):
		t.Fatal("no probe after the idle interval passed")
	}

	silent := newIdleLink(2*time.Second, 3*time.Second)
	defer close(silent.done)
	select {
	case <-waitProbeAsync(silent):
	case <-time.After(100 * time.Millisecond):
		t.Fatal("a link silent for the idle interval plus 1 s was not due a probe at once")
	}
}

// TestBusyLinkSendsNoProbe: frames received more often than the idle interval
// keep deferring the probe; it comes one interval after the last of them.
func TestBusyLinkSendsNoProbe(t *testing.T) {
	const idle = 300 * time.Millisecond
	s := newIdleLink(idle, 0)
	defer close(s.done)
	due := waitProbeAsync(s)

	busyUntil := time.Now().Add(time.Second)
	for time.Now().Before(busyUntil) {
		select {
		case <-due:
			t.Fatal("a link receiving a frame every 50 ms probed its peer")
		case <-time.After(50 * time.Millisecond):
			s.stampRecv(&frames.Reset{})
		}
	}
	last := time.Now()
	select {
	case at := <-due:
		if waited := at.Sub(last); waited < idle-50*time.Millisecond || waited > idle+200*time.Millisecond {
			t.Fatalf("probe came %v after the last frame; want about %v", waited, idle)
		}
	case <-time.After(effectTimeout):
		t.Fatal("no probe after the link went idle")
	}
}

// TestPongOnlyPeerIsProbedEveryN: a peer that sends nothing but pongs is still
// probed once per idle interval, and the link stays up.
func TestPongOnlyPeerIsProbedEveryN(t *testing.T) {
	const idle = 300 * time.Millisecond
	link, remote := newKeepaliveLink(t, &Module{}, idle, 200*time.Millisecond)
	peer := startProbePeer(t, remote, true)

	time.Sleep(pingJitter + 6*idle)
	probes := peer.received()
	if len(probes) < 4 {
		t.Fatalf("peer received %v probes in %v; want one per %v", len(probes), pingJitter+6*idle, idle)
	}
	for i := 1; i < len(probes); i++ {
		gap := probes[i].Sub(probes[i-1])
		if gap < idle-50*time.Millisecond || gap > idle+150*time.Millisecond {
			t.Fatalf("probe %v came %v after the previous one; want about %v", i, gap, idle)
		}
	}
	if err := link.Err(); err != nil {
		t.Fatalf("a link whose peer answers every probe closed: %v", err)
	}
}

// TestPeerProbeDoesNotRestartIdle: a probe from the peer is answered but does
// not count as hearing from it; any other frame does.
func TestPeerProbeDoesNotRestartIdle(t *testing.T) {
	link, remote := newKeepaliveLink(t, &Module{}, time.Hour, time.Hour)
	peer := startProbePeer(t, remote, false)
	before := link.lastRecv.Load()

	if err := peer.ch.Send(&frames.Ping{Nonce: astral.NewNonce()}); err != nil {
		t.Fatalf("send probe: %v", err)
	}
	select {
	case <-peer.pongs:
	case <-time.After(effectTimeout):
		t.Fatal("the link did not answer the peer's probe")
	}
	if got := link.lastRecv.Load(); got != before {
		t.Fatalf("the peer's probe moved the last receive time by %v", time.Duration(got-before))
	}

	if err := peer.ch.Send(&frames.Reset{Nonce: astral.NewNonce()}); err != nil {
		t.Fatalf("send reset: %v", err)
	}
	deadline := time.Now().Add(effectTimeout)
	for link.lastRecv.Load() == before {
		if time.Now().After(deadline) {
			t.Fatal("a reset frame from the peer did not move the last receive time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestPingFailsWhenWriteBlocks: a peer that stops reading blocks the probe's
// write; Ping still fails within the ping timeout plus 100 ms.
func TestPingFailsWhenWriteBlocks(t *testing.T) {
	const timeout = 300 * time.Millisecond
	local, remote := net.Pipe()
	defer remote.Close()
	defer local.Close()
	link := &Link{
		Channel:     channel.New(local, channel.WithLockedWrites()),
		done:        make(chan struct{}),
		pingTimeout: timeout,
	}

	start := time.Now()
	_, err := link.Ping()
	took := time.Since(start)
	if err == nil {
		t.Fatal("Ping over a conn nobody reads succeeded")
	}
	if took > timeout+100*time.Millisecond {
		t.Fatalf("Ping over a blocked write failed after %v; want at most %v", took, timeout+100*time.Millisecond)
	}
	link.pingMu.Lock()
	defer link.pingMu.Unlock()
	if link.ping != nil {
		t.Fatal("a failed Ping left its ping in flight")
	}
}

// TestSilentPeerClosesLink: a peer that reads but never answers gets its link
// closed within the idle interval plus the ping timeout, and the pool emits
// LinkClosedEvent for it.
func TestSilentPeerClosesLink(t *testing.T) {
	const idle, timeout = 300 * time.Millisecond, 200 * time.Millisecond
	localID := astral.GenerateIdentity()
	events := &recordingEvents{}
	mod := &Module{Deps: Deps{Events: events}, node: &identityNode{id: localID}, log: log.New(localID)}
	mod.linkPool = NewLinkPool(mod)
	link, remote := newKeepaliveLink(t, mod, idle, timeout)
	startProbePeer(t, remote, false)
	start := time.Now()
	if err := mod.linkPool.AddLink(link); err != nil {
		t.Fatalf("AddLink: %v", err)
	}

	select {
	case <-link.Done():
	case <-time.After(effectTimeout):
		t.Fatal("the link to a peer that never answers did not close")
	}
	if took, limit := time.Since(start), pingJitter+idle+timeout+200*time.Millisecond; took > limit {
		t.Fatalf("the link closed after %v; want at most %v", took, limit)
	}
	if err := link.Err(); err == nil || !strings.Contains(err.Error(), "ping timeout") {
		t.Fatalf("link closed with %v; want ping timeout", err)
	}

	deadline := time.Now().Add(effectTimeout)
	for !hasLinkClosed(events) {
		if time.Now().After(deadline) {
			t.Fatal("the pool emitted no LinkClosedEvent for the dead link")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func hasLinkClosed(events *recordingEvents) bool {
	for _, e := range events.recorded() {
		if _, ok := e.(*nodes.LinkClosedEvent); ok {
			return true
		}
	}
	return false
}

// TestLinksUseTheirTransportsKeepalive: links over two transports with
// different timing each probe at their own transport's interval, and a conn
// that sets no timing gets the defaults.
func TestLinksUseTheirTransportsKeepalive(t *testing.T) {
	const fast, slow, timeout = 250 * time.Millisecond, 600 * time.Millisecond, 400 * time.Millisecond
	fastLink, fastRemote := newKeepaliveLink(t, &Module{}, fast, timeout)
	slowLink, slowRemote := newKeepaliveLink(t, &Module{}, slow, 2*timeout)
	fastPeer := startProbePeer(t, fastRemote, true)
	slowPeer := startProbePeer(t, slowRemote, true)

	if fastLink.idlePing != fast || fastLink.pingTimeout != timeout {
		t.Fatalf("fast link timing %v/%v; want %v/%v", fastLink.idlePing, fastLink.pingTimeout, fast, timeout)
	}
	if slowLink.idlePing != slow || slowLink.pingTimeout != 2*timeout {
		t.Fatalf("slow link timing %v/%v; want %v/%v", slowLink.idlePing, slowLink.pingTimeout, slow, 2*timeout)
	}

	time.Sleep(pingJitter + 4*slow)
	for _, c := range []struct {
		name string
		peer *probePeer
		idle time.Duration
	}{{"fast", fastPeer, fast}, {"slow", slowPeer, slow}} {
		probes := c.peer.received()
		if len(probes) < 3 {
			t.Fatalf("%v link sent %v probes; want one per %v", c.name, len(probes), c.idle)
		}
		for i := 1; i < len(probes); i++ {
			gap := probes[i].Sub(probes[i-1])
			if gap < c.idle-50*time.Millisecond || gap > c.idle+150*time.Millisecond {
				t.Fatalf("%v link probe %v came %v after the previous one; want about %v", c.name, i, gap, c.idle)
			}
		}
	}

	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()
	if got := keepaliveOf(pipeIdentityConn{local}); got.Idle != defaultIdlePing || got.Timeout != defaultPingTimeout {
		t.Fatalf("a conn with no keepalive got %v; want %v/%v", got, defaultIdlePing, defaultPingTimeout)
	}
	half := keepaliveConn{pipeIdentityConn{local}, exonetmod.LinkKeepalive{Idle: fast}}
	if got := keepaliveOf(half); got.Idle != fast || got.Timeout != defaultPingTimeout {
		t.Fatalf("a conn setting only the interval got %v; want %v/%v", got, fast, defaultPingTimeout)
	}
}

// TestBusyLinkIsNeverProbed: over a whole link, a peer that sends a frame
// every third of the idle interval receives no probe across the start jitter
// and several intervals; once the peer falls silent, a probe reaches it.
func TestBusyLinkIsNeverProbed(t *testing.T) {
	const idle = 300 * time.Millisecond
	link, remote := newKeepaliveLink(t, &Module{}, idle, time.Second)
	peer := startProbePeer(t, remote, false)

	busyUntil := time.Now().Add(pingJitter + 4*idle)
	for time.Now().Before(busyUntil) {
		if err := peer.ch.Send(&frames.Reset{Nonce: astral.NewNonce()}); err != nil {
			t.Fatalf("send reset: %v", err)
		}
		time.Sleep(idle / 3)
	}
	if probes := peer.received(); len(probes) != 0 {
		t.Fatalf("a peer sending a frame every %v received %v probe(s)", idle/3, len(probes))
	}
	if err := link.Err(); err != nil {
		t.Fatalf("the busy link closed: %v", err)
	}

	// why: a probe after the silence shows the keepalive ran all along, so
	// the zero above is the busy link's doing.
	deadline := time.Now().Add(idle + 200*time.Millisecond)
	for len(peer.received()) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no probe within %v of the peer falling silent", idle+200*time.Millisecond)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestFrozenPeerFailsPendingQueryAtLinkClose: a peer frozen with a query
// pending (its socket still takes writes, nothing comes back) gets its link
// closed by the keepalive within the idle interval plus the ping timeout, and
// the pending query fails then as unavailable, not at its own deadline.
func TestFrozenPeerFailsPendingQueryAtLinkClose(t *testing.T) {
	const idle, timeout = 300 * time.Millisecond, 200 * time.Millisecond
	f := newProbeFixture()
	localID, remoteID := f.mod.node.Identity(), astral.GenerateIdentity()
	link, far := newPeerLink(t, f.mod, remoteID, idle, timeout)
	go io.Copy(io.Discard, far)
	if err := f.mod.linkPool.AddLink(link); err != nil {
		t.Fatalf("AddLink: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithIdentity(localID).WithTimeout(30 * time.Second)
	defer cancel()
	start := time.Now()
	_, err := link.RouteQuery(ctx, outcomeQuery(localID, remoteID), nopWriteCloser{})
	took := time.Since(start)

	assertRejectedWith(t, err, astral.CodeUnavailable)
	if limit := pingJitter + idle + timeout + 200*time.Millisecond; took > limit {
		t.Fatalf("the pending query failed after %v; want at most %v, the link's close", took, limit)
	}
	t.Logf("the pending query failed as unavailable after %v", took)
	if err := link.Err(); err == nil || !strings.Contains(err.Error(), "ping timeout") {
		t.Fatalf("link closed with %v; want ping timeout", err)
	}
}

// frameTap relays frames between two conns and counts the probes sent each way.
type frameTap struct {
	probes [2]atomic.Uint64
}

func (tap *frameTap) relay(from, to net.Conn, dir int) {
	in, out := channel.New(from), channel.New(to)
	for {
		obj, err := in.Receive()
		if err != nil {
			to.Close()
			return
		}
		if isProbe(obj) {
			tap.probes[dir].Add(1)
		}
		if out.Send(obj) != nil {
			from.Close()
			return
		}
	}
}

// TestIdleLinkPairProbesOncePerNPerEnd: two linked ends that send nothing but
// keepalive frames keep the link up across ten idle intervals, and each end
// sends about one probe per interval, two per interval in all.
func TestIdleLinkPairProbesOncePerNPerEnd(t *testing.T) {
	const idle, timeout = 200 * time.Millisecond, 200 * time.Millisecond
	// why sockets: a pipe has no buffer, and both ends answer probes from
	// their read loops, so a pipe through the relay can deadlock.
	aConn, aTap := tcpPair(t)
	bTap, bConn := tcpPair(t)
	tap := &frameTap{}
	go tap.relay(aTap, bTap, 0)
	go tap.relay(bTap, aTap, 1)
	k := exonetmod.LinkKeepalive{Idle: idle, Timeout: timeout}
	a := newLink(&Module{log: log.New(astral.Anyone)}, keepaliveConn{pipeIdentityConn{aConn}, k}, astral.NewNonce(), true)
	b := newLink(&Module{log: log.New(astral.Anyone)}, keepaliveConn{pipeIdentityConn{bConn}, k}, astral.NewNonce(), false)
	t.Cleanup(func() { a.Close(); b.Close() })

	// why: past the start jitter both ends probe on their idle schedule.
	time.Sleep(pingJitter + 2*idle)
	before := [2]uint64{tap.probes[0].Load(), tap.probes[1].Load()}
	const window = 10 * idle
	time.Sleep(window)

	for i, end := range []string{"a", "b"} {
		n := tap.probes[i].Load() - before[i]
		if want := uint64(window / idle); n+2 < want || n > want+1 {
			t.Fatalf("end %v sent %v probes in %v; want about %v, one per %v", end, n, window, want, idle)
		}
		t.Logf("end %v sent %v probes in %v", end, n, window)
	}
	for _, l := range []*Link{a, b} {
		if err := l.Err(); err != nil {
			t.Fatalf("an idle healthy link closed: %v", err)
		}
	}
}
