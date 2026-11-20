package nodes

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"github.com/astralp2p/astrald/mod/nodes/frames"
)

// peerCloseBound is how soon the peer must close its end after a close frame (D15).
const peerCloseBound = time.Second

// quietKeepalive keeps the keepalive out of a test: no probe within the test's run.
var quietKeepalive = exonetmod.LinkKeepalive{Idle: time.Minute, Timeout: time.Minute}

// silentCloseConn is a conn whose Close stops this end and sends the peer
// nothing, as a kcp close does: the peer's reads see no EOF.
type silentCloseConn struct {
	net.Conn
	closed atomic.Bool
}

func (c *silentCloseConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		// why a deadline: it ends this end's blocked reads and writes without a FIN.
		_ = c.Conn.SetDeadline(time.Now())
	}
	return nil
}

// silentLink runs a link over conn, wrapped so that closing the link sends the peer no EOF.
func silentLink(t *testing.T, conn net.Conn, outbound bool) *Link {
	t.Helper()
	mod := &Module{log: log.New(astral.Anyone)}
	c := keepaliveConn{pipeIdentityConn{&silentCloseConn{Conn: conn}}, quietKeepalive}
	link := newLink(mod, c, astral.NewNonce(), outbound)
	t.Cleanup(func() { link.Close() })
	return link
}

// silentLinkPair returns both ends of one link over a transport that delivers no EOF on close.
func silentLinkPair(t *testing.T) (a, b *Link) {
	t.Helper()
	ca, cb := tcpPair(t)
	return silentLink(t, ca, true), silentLink(t, cb, false)
}

func waitDone(t *testing.T, link *Link, within time.Duration, what string) {
	t.Helper()
	select {
	case <-link.Done():
	case <-time.After(within):
		t.Fatalf("%v: link still open after %v (err %v)", what, within, link.Err())
	}
}

// TestPeerClosesOnCloseFrame: closing one end makes the peer close within 1 s
// with ErrPeerClosed naming the reason CloseWithError maps from its error,
// over a transport that delivers no EOF on close.
func TestPeerClosesOnCloseFrame(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want frames.CloseReason
	}{
		{"excess", nodesmod.ErrExcessLink, frames.CloseExcess},
		{"shutdown", context.Canceled, frames.CloseShutdown},
		{"other error", errors.New("node expelled"), frames.CloseClosed},
		{"nil error", nil, frames.CloseClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := silentLinkPair(t)
			if err := a.CloseWithError(tc.err); err != nil {
				t.Fatalf("CloseWithError: %v", err)
			}
			waitDone(t, b, peerCloseBound, "peer after the close frame")

			var closed *nodesmod.ErrPeerClosed
			if !errors.As(b.Err(), &closed) {
				t.Fatalf("peer link closed with %v; want ErrPeerClosed", b.Err())
			}
			if closed.Reason != tc.want {
				t.Fatalf("peer link closed with reason %v; want %v", closed.Reason, tc.want)
			}
			waitDone(t, a, effectTimeout, "closing end")
		})
	}
}

// TestKCPPeerClosesOnCloseFrame: over a real KCP session, which carries no
// end-of-stream, the dialer's excess close makes the accepting end close
// within 1 s with ErrPeerClosed (excess), long before its keepalive would.
func TestKCPPeerClosesOnCloseFrame(t *testing.T) {
	pair := newKCPLinkPair(t, quietKeepalive)
	if err := pair.outbound.CloseWithError(nodesmod.ErrExcessLink); err != nil {
		t.Fatalf("CloseWithError: %v", err)
	}
	waitDone(t, pair.inbound, peerCloseBound, "accepting end after the close frame")
	var closed *nodesmod.ErrPeerClosed
	if !errors.As(pair.inbound.Err(), &closed) || closed.Reason != frames.CloseExcess {
		t.Fatalf("accepting end closed with %v; want ErrPeerClosed (excess)", pair.inbound.Err())
	}
}

// TestSilentCloseDeliversNoEOF pins the test transport: a close without the
// frame leaves the peer open, so only the frame closes the peer above.
func TestSilentCloseDeliversNoEOF(t *testing.T) {
	a, b := silentLinkPair(t)
	_ = a.Channel.Close()
	waitDone(t, a, effectTimeout, "closing end")
	select {
	case <-b.Done():
		t.Fatalf("peer closed with %v after a close that sent no frame; the transport delivers EOF", b.Err())
	case <-time.After(peerCloseBound):
	}
}

// TestCloseFrameGetsNoAnswer: a link that receives a close frame closes and
// sends nothing back, also when it is closed again afterwards.
func TestCloseFrameGetsNoAnswer(t *testing.T) {
	ca, cb := tcpPair(t)
	link := silentLink(t, cb, false)
	if err := channel.New(ca).Send(&frames.Close{Reason: frames.CloseExcess}); err != nil {
		t.Fatalf("sending the close frame: %v", err)
	}
	waitDone(t, link, peerCloseBound, "link after a close frame")
	_ = link.CloseWithError(errors.New("late close"))

	_ = ca.SetReadDeadline(time.Now().Add(absenceWindow))
	if n, err := ca.Read(make([]byte, 1)); n > 0 || !isTimeout(err) {
		t.Fatalf("peer read %v bytes, err %v; want nothing until the deadline", n, err)
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// TestCloseWithErrorBoundedOnBlockedWriter: with a writer stuck on a peer
// that never reads, CloseWithError returns within closeNoticeBound and the
// link closes.
func TestCloseWithErrorBoundedOnBlockedWriter(t *testing.T) {
	local, remote := net.Pipe()
	t.Cleanup(func() { local.Close(); remote.Close() })
	link := newLink(&Module{log: log.New(astral.Anyone)}, keepaliveConn{pipeIdentityConn{local}, quietKeepalive}, astral.NewNonce(), true)

	// why one byte: net.Pipe is unbuffered, so the writer holds the write lock
	// mid-frame from here on, and the peer reads nothing more.
	stuck := make(chan error, 1)
	go func() { stuck <- link.Send(&frames.Data{Nonce: astral.NewNonce(), Payload: make([]byte, 1024)}) }()
	if _, err := remote.Read(make([]byte, 1)); err != nil {
		t.Fatalf("reading the first byte of the stuck frame: %v", err)
	}

	start := time.Now()
	closeErr := errors.New("closed with a stuck writer")
	_ = link.CloseWithError(closeErr)
	if took, limit := time.Since(start), closeNoticeBound+500*time.Millisecond; took > limit {
		t.Fatalf("CloseWithError returned after %v; want within %v", took, limit)
	}
	waitDone(t, link, effectTimeout, "link with a stuck writer")
	if !errors.Is(link.Err(), closeErr) {
		t.Fatalf("link closed with %v; want %v", link.Err(), closeErr)
	}
	select {
	case <-stuck:
	case <-time.After(effectTimeout):
		t.Fatal("the stuck writer did not return after the close")
	}
}

// TestUnknownFrameTypeClosesLink: a receiver without a frame type (a peer of
// an older version receiving close) ends its read loop and closes the link.
func TestUnknownFrameTypeClosesLink(t *testing.T) {
	ca, cb := tcpPair(t)
	link := silentLink(t, cb, false)

	var frame []byte
	frame = append(frame, byte(len("nodes.frames.future")))
	frame = append(frame, "nodes.frames.future"...)
	frame = append(frame, 0, 0, 0, 1, 2) // bytes32 payload: length 1, value 2
	if _, err := ca.Write(frame); err != nil {
		t.Fatalf("writing the unknown frame: %v", err)
	}

	waitDone(t, link, peerCloseBound, "link after an unknown frame type")
	if !errors.Is(link.Err(), astral.ErrBlueprintNotFound) {
		t.Fatalf("link closed with %v; want %v", link.Err(), astral.ErrBlueprintNotFound)
	}
}

// routingNode is an identityNode that routes inbound queries to router.
type routingNode struct {
	identityNode
	router astral.Router
}

func (n *routingNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	return n.router.RouteQuery(ctx, q, w)
}

// silentPipeTo is pipeTo over a transport that delivers no EOF on close.
func (to *linkEnd) silentPipeTo(t *testing.T) exonetmod.Conn {
	t.Helper()
	a, b := tcpPair(t)
	go func() {
		_ = to.mod.EstablishInboundLink(context.Background(), &negotiationPipeConn{Conn: &silentCloseConn{Conn: b}})
	}()
	return &negotiationPipeConn{Conn: &silentCloseConn{Conn: a}, outbound: true}
}

// silentDial is dial over silentPipeTo.
func (from *linkEnd) silentDial(t *testing.T, to *linkEnd) *Link {
	t.Helper()
	raw, err := from.mod.EstablishOutboundLink(context.Background(), to.id, to.silentPipeTo(t))
	if err != nil {
		t.Fatalf("EstablishOutboundLink: %v", err)
	}
	link := raw.(*Link)
	waitFor(t, "the inbound end to register the link", func() bool { return to.find(link.id) != nil })
	return link
}

// waitGone fails unless end drops link id from its pool within peerCloseBound.
func waitGone(t *testing.T, end *linkEnd, id astral.Nonce, who string) {
	t.Helper()
	deadline := time.Now().Add(peerCloseBound)
	for end.find(id) != nil {
		if time.Now().After(deadline) {
			t.Fatalf("%v still holds the excess link %v %v after it was closed", who, id, peerCloseBound)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestExcessLinkDropsAtBothEnds: when the pool settles an excess link over a
// transport with no EOF, both ends drop the loser within 1 s, and a query the
// other end routes right after goes over the survivor and completes.
func TestExcessLinkDropsAtBothEnds(t *testing.T) {
	lower, higher := newLinkPair(t)
	router := &answeringRouter{reply: "answer", routed: make(chan *astral.InFlightQuery, 1)}
	lower.mod.node = &routingNode{identityNode: identityNode{id: lower.id}, router: router}

	kept := lower.silentDial(t, higher)
	lower.mod.linkPool.offerStrategyLink(kept, nodesmod.StrategyBasic)
	loser := lower.silentDial(t, higher)
	loserAtPeer := higher.find(loser.id)
	lower.mod.linkPool.offerStrategyLink(loser, nodesmod.StrategyBasic)

	if !errors.Is(loser.Err(), nodesmod.ErrExcessLink) {
		t.Fatalf("the second link closed with %v; want %v", loser.Err(), nodesmod.ErrExcessLink)
	}
	waitGone(t, lower, loser.id, "the settling end")
	waitGone(t, higher, loser.id, "the peer")
	var closed *nodesmod.ErrPeerClosed
	if !errors.As(loserAtPeer.Err(), &closed) || closed.Reason != frames.CloseExcess {
		t.Fatalf("the peer closed the excess link with %v; want ErrPeerClosed (excess)", loserAtPeer.Err())
	}
	if remote := higher.mod.linkPool.SelectLinkWith(lower.id); remote == nil || remote.id != kept.id {
		t.Fatalf("the peer selects %v; want the survivor %v", remote, kept.id)
	}

	ctx, cancel := astral.NewContext(nil).WithIdentity(higher.id).IncludeZone(astral.ZoneNetwork).WithTimeout(effectTimeout)
	defer cancel()
	q := astral.Launch(&astral.Query{Nonce: astral.NewNonce(), Caller: higher.id, Target: lower.id, QueryString: "app.op"})
	pr, pw := io.Pipe()
	if _, err := higher.mod.RouteQuery(ctx, q, pw); err != nil {
		t.Fatalf("RouteQuery over the survivor: %v", err)
	}
	select {
	case <-router.routed:
	case <-time.After(effectTimeout):
		t.Fatal("the query never reached the settling end")
	}
	answer := make(chan string, 1)
	go func() {
		buf, _ := io.ReadAll(pr)
		answer <- string(buf)
	}()
	select {
	case got := <-answer:
		if got != "answer" {
			t.Fatalf("caller read %q; want %q", got, "answer")
		}
	case <-time.After(effectTimeout):
		t.Fatal("the answer never reached the caller")
	}
}
