package nodes

import (
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

type pipeIdentityConn struct {
	net.Conn
}

func (c pipeIdentityConn) LocalIdentity() *astral.Identity  { return astral.Anyone }
func (c pipeIdentityConn) RemoteIdentity() *astral.Identity { return astral.Anyone }

func testPipeLink(t *testing.T) (*Link, net.Conn) {
	t.Helper()
	local, remote := net.Pipe()
	t.Cleanup(func() { local.Close(); remote.Close() })
	return newLink(&Module{}, pipeIdentityConn{local}, astral.NewNonce(), false), remote
}

func assertDisconnected(t *testing.T, link *Link, peer net.Conn) {
	t.Helper()
	peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("peer did not receive EOF: %v", err)
	}
	select {
	case <-link.Done():
	case <-time.After(time.Second):
		t.Fatal("link reader did not stop")
	}
}

func TestLinkPoolShutdownDisconnectsPeers(t *testing.T) {
	pool := NewLinkPool(nil)
	link, peer := testPipeLink(t)
	pool.links.Add(link)
	pool.Close()
	pool.Close()
	assertDisconnected(t, link, peer)
	if pool.SelectLinkWith(astral.Anyone) != nil {
		t.Fatal("stopped pool still reports an online peer")
	}
	if !errors.Is(link.Err(), context.Canceled) {
		t.Fatalf("unexpected shutdown error: %v", link.Err())
	}
}

func TestLinkPoolRejectsHandshakeCompletedAfterShutdown(t *testing.T) {
	pool := NewLinkPool(nil)
	pool.Close()
	link, peer := testPipeLink(t)
	if err := pool.AddLink(link); !errors.Is(err, context.Canceled) {
		t.Fatalf("late link accepted after shutdown: %v", err)
	}
	assertDisconnected(t, link, peer)
	if len(pool.links.Clone()) != 0 {
		t.Fatal("late handshake left a link in the pool")
	}
}

// newOutboundLinks builds eight outbound links from localID to remoteID on mod; the
// test's cleanup ends each link.
func newOutboundLinks(t *testing.T, mod *Module, localID, remoteID *astral.Identity) []*Link {
	t.Helper()

	links := make([]*Link, 8)
	for i := range links {
		ch := channel.New(&recordingLink{}, channel.WithLockedWrites())
		links[i] = &Link{
			id:       astral.NewNonce(),
			conn:     &identityConn{remote: remoteID},
			mux:      newMux(mod, ch, localID, remoteID, nil, nil),
			done:     make(chan struct{}),
			outbound: true,
		}
	}
	t.Cleanup(func() {
		for _, l := range links {
			close(l.done)
		}
	})
	return links
}

// TestAddLinkCountsConcurrentLinksOnce: of links to one node added at once,
// exactly one reports itself as the first link, and the counts run 1 to n.
// note: the user module syncs on LinkCount 1 alone (mod/user/src/object_receiver.go),
// so a count no link reports as 1 skips the first-link sync.
func TestAddLinkCountsConcurrentLinksOnce(t *testing.T) {
	localID, remoteID := astral.GenerateIdentity(), astral.GenerateIdentity()
	events := &recordingEvents{}
	mod := &Module{Deps: Deps{Events: events}, node: &identityNode{id: localID}, log: log.New(localID)}
	mod.linkPool = NewLinkPool(mod)
	links := newOutboundLinks(t, mod, localID, remoteID)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, l := range links {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := mod.linkPool.AddLink(l); err != nil {
				t.Errorf("AddLink: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	var counts []int
	for _, e := range events.recorded() {
		if created, ok := e.(*nodes.LinkCreatedEvent); ok {
			counts = append(counts, int(created.LinkCount))
		}
	}
	slices.Sort(counts)

	want := make([]int, len(links))
	for i := range want {
		want[i] = i + 1
	}
	if !slices.Equal(counts, want) {
		t.Fatalf("LinkCreatedEvent counts %v; want %v", counts, want)
	}
}

// probeFixture is a pool on a module that records its events.
type probeFixture struct {
	mod    *Module
	events *recordingEvents
}

func newProbeFixture() *probeFixture {
	localID := astral.GenerateIdentity()
	events := &recordingEvents{}
	mod := &Module{Deps: Deps{Events: events}, node: &identityNode{id: localID}, log: log.New(localID)}
	mod.linkPool = NewLinkPool(mod)
	return &probeFixture{mod: mod, events: events}
}

// addIdleLink adds a link to the shared pipe identity that never probes on its
// own, with the ping timeout given, and starts its peer.
func (f *probeFixture) addIdleLink(t *testing.T, timeout time.Duration, answer bool) (*Link, *probePeer) {
	t.Helper()
	link, remote := newKeepaliveLink(t, f.mod, time.Hour, timeout)
	peer := startProbePeer(t, remote, answer)
	if err := f.mod.linkPool.AddLink(link); err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	return link, peer
}

// closedEvents returns the LinkClosedEvents emitted so far.
func (f *probeFixture) closedEvents() []*nodes.LinkClosedEvent {
	var closed []*nodes.LinkClosedEvent
	for _, e := range f.events.recorded() {
		if c, ok := e.(*nodes.LinkClosedEvent); ok {
			closed = append(closed, c)
		}
	}
	return closed
}

// TestNewLinkClosesDeadOlderLink: a second link to a peer makes the pool ping
// the first; a first link whose peer never answers closes within its ping
// timeout plus 1 s, and the pool reports one link left.
func TestNewLinkClosesDeadOlderLink(t *testing.T) {
	const timeout = 300 * time.Millisecond
	f := newProbeFixture()
	old, oldPeer := f.addIdleLink(t, timeout, false)

	// why: past the start jitter, the old link's loop waits for a probe trigger.
	time.Sleep(pingJitter + 100*time.Millisecond)
	if n := len(oldPeer.received()); n != 0 {
		t.Fatalf("the only link to a peer sent %v probes before a second link arrived; want none", n)
	}

	start := time.Now()
	replacement, _ := f.addIdleLink(t, time.Hour, true)

	select {
	case <-old.Done():
	case <-time.After(timeout + time.Second):
		t.Fatalf("the dead older link was still open %v after a new link arrived", timeout+time.Second)
	}
	if took := time.Since(start); took > timeout+time.Second {
		t.Fatalf("the dead older link closed %v after the new link; want at most %v", took, timeout+time.Second)
	}
	if n := len(oldPeer.received()); n != 1 {
		t.Fatalf("the older link sent %v probes; want one", n)
	}

	eventually(t, "a LinkClosedEvent for the dead link", func() bool { return len(f.closedEvents()) > 0 })
	closed := f.closedEvents()
	if len(closed) != 1 || closed[0].LinkCount != 1 {
		t.Fatalf("LinkClosedEvents %v; want one with LinkCount 1", closed)
	}
	if err := replacement.Err(); err != nil {
		t.Fatalf("the new link closed: %v", err)
	}
}

// TestNewLinkKeepsHealthyOlderLink: a first link whose peer answers gets one
// ping when a second link arrives, stays open, and no LinkClosedEvent follows.
func TestNewLinkKeepsHealthyOlderLink(t *testing.T) {
	const timeout = 300 * time.Millisecond
	f := newProbeFixture()
	old, oldPeer := f.addIdleLink(t, timeout, true)

	time.Sleep(pingJitter + 100*time.Millisecond)
	f.addIdleLink(t, time.Hour, true)

	eventually(t, "a probe on the older link", func() bool { return len(oldPeer.received()) > 0 })
	time.Sleep(timeout + absenceWindow)

	if err := old.Err(); err != nil {
		t.Fatalf("the healthy older link closed: %v", err)
	}
	if n := len(oldPeer.received()); n != 1 {
		t.Fatalf("the older link sent %v probes; want one", n)
	}
	if closed := f.closedEvents(); len(closed) != 0 {
		t.Fatalf("LinkClosedEvents %v; want none", closed)
	}
}

// peerConn is a pipe conn to the node remote, over a transport that sets the keepalive k.
type peerConn struct {
	keepaliveConn
	remote *astral.Identity
}

func (c peerConn) RemoteIdentity() *astral.Identity { return c.remote }

// newPeerLink starts a link to remote over a pipe whose transport sets idle and timeout.
func newPeerLink(t *testing.T, mod *Module, remote *astral.Identity, idle, timeout time.Duration) (*Link, net.Conn) {
	t.Helper()
	local, far := net.Pipe()
	t.Cleanup(func() { local.Close(); far.Close() })
	k := exonetmod.LinkKeepalive{Idle: idle, Timeout: timeout}
	conn := peerConn{keepaliveConn{pipeIdentityConn{local}, k}, remote}
	return newLink(mod, conn, astral.NewNonce(), false), far
}

// closedWith returns the LinkClosedEvents the pool emitted for links to id.
func closedWith(events *recordingEvents, id *astral.Identity) []*nodes.LinkClosedEvent {
	var closed []*nodes.LinkClosedEvent
	for _, e := range events.recorded() {
		if c, ok := e.(*nodes.LinkClosedEvent); ok && c.RemoteIdentity.IsEqual(id) {
			closed = append(closed, c)
		}
	}
	return closed
}

// TestDeadLinkGivesWayToLiveOne: of two links to one peer, the older one's
// peer stops reading. While the older link is pooled, SelectLinkWith returns
// it. The keepalive closes it within the transport's idle interval plus ping
// timeout, the pool emits LinkClosedEvent with one link left, and
// SelectLinkWith then returns the newer link.
// note: this is A1G2 option a; selection itself is unchanged
// (https://wiki.satforge.dev/doc/a1g2-route-over-a-working-link-when-a-peer-has-a-dead-one-aVNtmu7SWl).
// note: adding the newer link also wakes the older link's ping
// (probeOlderLinks), which can close it within the ping timeout alone; the
// bound of idle interval plus ping timeout covers both paths.
func TestDeadLinkGivesWayToLiveOne(t *testing.T) {
	const idle, timeout = 300 * time.Millisecond, 200 * time.Millisecond
	localID, remoteID := astral.GenerateIdentity(), astral.GenerateIdentity()
	events := &recordingEvents{}
	mod := &Module{Deps: Deps{Events: events}, node: &identityNode{id: localID}, log: log.New(localID)}
	mod.linkPool = NewLinkPool(mod)

	// why no reader on the older link's far end: a peer gone from the network
	// leaves the probe's write stuck, as data stuck in flight does in the field.
	dead, _ := newPeerLink(t, mod, remoteID, idle, timeout)
	live, liveFar := newPeerLink(t, mod, remoteID, idle, timeout)
	startProbePeer(t, liveFar, true)

	start := time.Now()
	for _, l := range []*Link{dead, live} {
		if err := mod.linkPool.AddLink(l); err != nil {
			t.Fatalf("AddLink: %v", err)
		}
	}
	if got := mod.linkPool.SelectLinkWith(remoteID); got != dead {
		t.Fatal("SelectLinkWith did not return the older link while both were pooled")
	}

	select {
	case <-dead.Done():
	case <-time.After(effectTimeout):
		t.Fatal("the link to a peer that stopped reading did not close")
	}
	if took, limit := time.Since(start), pingJitter+idle+timeout+200*time.Millisecond; took > limit {
		t.Fatalf("the dead link closed after %v; want at most %v", took, limit)
	}
	if err := dead.Err(); err == nil || !strings.Contains(err.Error(), "ping timeout") {
		t.Fatalf("the dead link closed with %v; want ping timeout", err)
	}

	deadline := time.Now().Add(effectTimeout)
	for len(closedWith(events, remoteID)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the pool emitted no LinkClosedEvent for the dead link")
		}
		time.Sleep(10 * time.Millisecond)
	}
	closed := closedWith(events, remoteID)
	if len(closed) != 1 || closed[0].LinkCount != 1 {
		t.Fatalf("LinkClosedEvents for the peer: %v; want one with LinkCount 1", closed)
	}

	if got := mod.linkPool.SelectLinkWith(remoteID); got != live {
		t.Fatal("SelectLinkWith did not return the live link after the dead one closed")
	}
	if err := live.Err(); err != nil {
		t.Fatalf("the live link closed: %v", err)
	}
}
