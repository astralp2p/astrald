package nodes

import (
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	kcpgo "github.com/xtaci/kcp-go/v5"
)

// loopbackEvents records each emitted event and hands it to the module's own
// ReceiveObject as an event this node sent, as mod/events delivers it.
type loopbackEvents struct {
	recordingEvents
	mod *Module
}

func (e *loopbackEvents) Emit(data astral.Object) *events.Event {
	e.recordingEvents.Emit(data)
	self := e.mod.node.Identity()
	event := &events.Event{ID: astral.NewNonce(), SourceID: self, Data: data}
	go e.mod.ReceiveObject(&recordingDrop{sender: self, object: event})
	return event
}

// routedPull is one remote resolve call and the link the pool selected for
// the resolver when it started.
type routedPull struct {
	pull
	via *Link
}

// moveFixture is a nodes module with a link pool whose events reach its own
// receiver, a local swarm of this node and peer, and a fake remote resolver
// that answers peer's current address.
type moveFixture struct {
	mod    *Module
	peer   *astral.Identity
	events *loopbackEvents
	pulls  chan routedPull

	mu   sync.Mutex
	addr string
}

func newMoveFixture(t *testing.T) *moveFixture {
	t.Helper()
	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	self := astral.GenerateIdentity()
	f := &moveFixture{peer: astral.GenerateIdentity(), events: &loopbackEvents{}, pulls: make(chan routedPull, 16)}
	f.mod = &Module{
		Deps: Deps{Events: f.events, User: &swarmUser{swarm: []*astral.Identity{self, f.peer}}},
		node: &identityNode{id: self},
		log:  log.New(self),
		ctx:  ctx,
		db:   testEndpointDB(t),
	}
	f.events.mod = f.mod
	f.mod.resolveRemote = f.resolveRemote
	f.mod.linkPool = NewLinkPool(f.mod)
	return f
}

func (f *moveFixture) resolveRemote(_ *astral.Context, resolver, identity *astral.Identity) ([]*nodes.EndpointWithTTL, error) {
	f.pulls <- routedPull{pull{resolver: resolver, identity: identity}, f.mod.linkPool.SelectLinkWith(resolver)}
	f.mu.Lock()
	defer f.mu.Unlock()
	return endpointsWithTTL(f.addr), nil
}

func (f *moveFixture) setAddr(addr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addr = addr
}

// requirePull waits for one pull of the peer's endpoints from the peer and returns it.
func (f *moveFixture) requirePull(t *testing.T) routedPull {
	t.Helper()
	select {
	case p := <-f.pulls:
		if !p.resolver.IsEqual(f.peer) || !p.identity.IsEqual(f.peer) {
			t.Fatalf("pulled endpoints of %v from %v; want the peer's from the peer", p.identity, p.resolver)
		}
		return p
	case <-time.After(effectTimeout):
		t.Fatal("no pull of the peer's endpoints started")
		return routedPull{}
	}
}

// holds reports whether the endpoint table holds an unexpired row for addr of the peer.
func (f *moveFixture) holds(addr string) bool {
	rows, err := f.mod.db.FindEndpoints(f.peer)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.Address == tcpEndpoint(addr).Address() {
			return true
		}
	}
	return false
}

// TestMovedPeerNewLinkDropsOldLinkAndRefreshesEndpoints (AC6, netsim
// leave-lan half, in process): the peer leaves the LAN, so its old link
// black-holes, and links again from a new address. The old link closes within
// its ping timeout plus the close-notice bound plus 1 s of the new link (35 s in the field), and the
// endpoint table holds the peer's new address within the ping timeout plus
// 2 s (60 s in the field), pulled over the new link, with no close_link.
func TestMovedPeerNewLinkDropsOldLinkAndRefreshesEndpoints(t *testing.T) {
	const timeout = 300 * time.Millisecond
	f := newMoveFixture(t)
	f.setAddr("10.77.0.2")

	// why no reader on the old link's far end: the peer left the LAN, and the
	// old path swallows the probe as a dead path does in the field.
	old, _ := newPeerLink(t, f.mod, f.peer, time.Hour, timeout)
	if err := f.mod.linkPool.AddLink(old); err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	f.requirePull(t)
	eventually(t, "the peer's LAN address in the table", func() bool { return f.holds("10.77.0.2") })

	// why: past the start jitter, the old link's loop waits for a probe trigger.
	time.Sleep(pingJitter + 100*time.Millisecond)
	f.setAddr("10.78.0.2")
	live, liveFar := newPeerLink(t, f.mod, f.peer, time.Hour, timeout)
	startProbePeer(t, liveFar, true)

	start := time.Now()
	if err := f.mod.linkPool.AddLink(live); err != nil {
		t.Fatalf("AddLink: %v", err)
	}

	// why: the new link is the newest and healthy, so queries leave the black-holed link at once, before it closes.
	if got := f.mod.linkPool.SelectLinkWith(f.peer); got != live {
		t.Fatal("SelectLinkWith did not return the new link while the old link was still pooled")
	}

	// why closeNoticeBound: the old link's far end never reads, so the close frame's write waits out its bound.
	select {
	case <-old.Done():
	case <-time.After(timeout + closeNoticeBound + time.Second):
		t.Fatalf("the old link was still open %v after the new link", timeout+closeNoticeBound+time.Second)
	}
	if err := old.Err(); !isStuckProbeCause(err) {
		t.Fatalf("the old link closed with %v; want ping timeout or %v", err, nodesmod.ErrWriteStalled)
	}

	if p := f.requirePull(t); p.via != live {
		t.Fatal("the pull after the close did not go over the new link")
	}
	eventually(t, "the peer's new address in the table", func() bool { return f.holds("10.78.0.2") })
	if took, bound := time.Since(start), timeout+2*time.Second; took > bound {
		t.Fatalf("the table held the new address %v after the new link; want at most %v", took, bound)
	}
	if err := live.Err(); err != nil {
		t.Fatalf("the new link closed: %v", err)
	}
}

// killablePacketConn is a UDP socket that drops every packet both ways once
// killed, as the socket of a process killed without a goodbye.
// why: kcp sends no close notice, so the far end of a killed process sees silence.
type killablePacketConn struct {
	net.PacketConn
	killed atomic.Bool
}

func (c *killablePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		n, addr, err := c.PacketConn.ReadFrom(p)
		if err != nil || !c.killed.Load() {
			return n, addr, err
		}
	}
}

func (c *killablePacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if c.killed.Load() {
		return len(p), nil
	}
	return c.PacketConn.WriteTo(p, addr)
}

// kcpLinkConn is a kcp session carrying a link between two identities, over a
// transport that sets the keepalive k.
type kcpLinkConn struct {
	*kcpgo.UDPSession
	local, remote *astral.Identity
	k             exonetmod.LinkKeepalive
}

func (c *kcpLinkConn) LocalIdentity() *astral.Identity        { return c.local }
func (c *kcpLinkConn) RemoteIdentity() *astral.Identity       { return c.remote }
func (c *kcpLinkConn) LinkKeepalive() exonetmod.LinkKeepalive { return c.k }

// kcpProcess is one run of the peer's process: a kcp listener on loopback whose
// accepted session serves a mux that answers pings and queries.
type kcpProcess struct {
	conn   *killablePacketConn
	router *answeringRouter
}

func startKcpProcess(t *testing.T, peerID, nodeID *astral.Identity) *kcpProcess {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	p := &kcpProcess{
		conn:   &killablePacketConn{PacketConn: pc},
		router: &answeringRouter{reply: "answer", routed: make(chan *astral.InFlightQuery, 1)},
	}
	l, err := kcpgo.ServeConn(nil, 0, 0, p.conn)
	if err != nil {
		t.Fatalf("serve kcp: %v", err)
	}
	t.Cleanup(func() { l.Close(); pc.Close() })

	go func() {
		sess, err := l.AcceptKCP()
		if err != nil {
			return
		}
		t.Cleanup(func() { sess.Close() })
		sess.SetNoDelay(1, 10, 2, 1)
		testMux(t, sess, peerID, nodeID).SetRouter(p.router)
	}()
	return p
}

// kill stops the process without a goodbye.
func (p *kcpProcess) kill() { p.conn.killed.Store(true) }

// dialKcpLink dials process p and returns an outbound link from nodeID to
// peerID over the kcp session, probed only on a wake, with ping timeout timeout.
// One ping proves the session before the link is pooled, as a handshake would.
func dialKcpLink(t *testing.T, mod *Module, p *kcpProcess, peerID *astral.Identity, timeout time.Duration) *Link {
	t.Helper()
	sess, err := kcpgo.DialWithOptions(p.conn.LocalAddr().String(), nil, 0, 0)
	if err != nil {
		t.Fatalf("dial kcp: %v", err)
	}
	sess.SetNoDelay(1, 10, 2, 1)
	t.Cleanup(func() { sess.Close() })

	k := exonetmod.LinkKeepalive{Idle: time.Hour, Timeout: timeout}
	link := newLink(mod, &kcpLinkConn{sess, mod.node.Identity(), peerID, k}, astral.NewNonce(), true)
	if _, err := link.Ping(); err != nil {
		t.Fatalf("the kcp link to the peer's process does not answer: %v", err)
	}
	return link
}

// TestRestartedKcpPeerStaleLinkClosesAndQueryAnswers (AC7, netsim S5 on kcp,
// in process over loopback UDP): the peer's process is killed without a
// goodbye and starts again; the new process's link arrives while the stale kcp
// link is pooled. The stale link closes within its ping timeout plus 1 s of
// the new link, with no close_link, and a query to the peer is then answered
// by the new process.
// note: kcp's default ping timeout is 10 s (mod/kcp TestLinkKeepaliveConfig),
// so the field bound is 11 s, inside the criterion's 35 s.
func TestRestartedKcpPeerStaleLinkClosesAndQueryAnswers(t *testing.T) {
	const timeout = time.Second
	f := newProbeFixture()
	self, peer := f.mod.node.Identity(), astral.GenerateIdentity()

	first := startKcpProcess(t, peer, self)
	stale := dialKcpLink(t, f.mod, first, peer, timeout)
	if err := f.mod.linkPool.AddLink(stale); err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	// why: past the start jitter, the stale link's loop waits for a probe trigger.
	time.Sleep(pingJitter + 100*time.Millisecond)

	first.kill()
	second := startKcpProcess(t, peer, self)
	replacement := dialKcpLink(t, f.mod, second, peer, timeout)

	if err := f.mod.linkPool.AddLink(replacement); err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	select {
	case <-stale.Done():
	case <-time.After(timeout + time.Second):
		t.Fatalf("the stale kcp link was still open %v after the new link", timeout+time.Second)
	}
	if err := stale.Err(); err == nil || !strings.Contains(err.Error(), "ping timeout") {
		t.Fatalf("the stale kcp link closed with %v; want ping timeout", err)
	}
	eventually(t, "a LinkClosedEvent for the stale link", func() bool { return len(f.closedEvents()) > 0 })
	if closed := f.closedEvents(); len(closed) != 1 || closed[0].LinkCount != 1 {
		t.Fatalf("LinkClosedEvents %v; want one with LinkCount 1", closed)
	}

	requireQueryAnswered(t, f.mod, peer, second.router)
	select {
	case <-first.router.routed:
		t.Fatal("the killed process received the query")
	default:
	}
}

// requireQueryAnswered routes a query from mod's node to peer and checks that
// router routed it and its answer reached the caller.
func requireQueryAnswered(t *testing.T, mod *Module, peer *astral.Identity, router *answeringRouter) {
	t.Helper()
	self := mod.node.Identity()
	ctx, cancel := astral.NewContext(nil).WithIdentity(self).IncludeZone(astral.ZoneNetwork).WithTimeout(effectTimeout)
	defer cancel()

	pr, pw := io.Pipe()
	if _, err := mod.RouteQuery(ctx, outcomeQuery(self, peer), pw); err != nil {
		t.Fatalf("RouteQuery: %v; want the peer to answer", err)
	}
	select {
	case <-router.routed:
	case <-time.After(effectTimeout):
		t.Fatal("the peer never received the query")
	}

	answer := make(chan string, 1)
	go func() {
		buf, _ := io.ReadAll(pr)
		answer <- string(buf)
	}()
	select {
	case got := <-answer:
		if got != "answer" {
			t.Fatalf("the caller read %q; want %q", got, "answer")
		}
	case <-time.After(effectTimeout):
		t.Fatal("the peer's answer never reached the caller")
	}
}
