package nodes

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	kcpsrc "github.com/astralp2p/astrald/mod/kcp/src"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"github.com/astralp2p/astrald/mod/nodes/frames"
	kcpgo "github.com/xtaci/kcp-go/v5"
)

// TestKCPCloseFrameOnFreshLink: a dialer that closes a KCP link as excess
// right after it is established, as the basic strategy does with a second
// row, still makes the accepting end close within 1 s with ErrPeerClosed
// (excess), and the dialer's link ends at once.
//
// why fresh: a new KCP session's congestion window opens one segment per
// acknowledgment, so the close frame queues behind the dialer's unacknowledged
// handshake segments; a close that ends the session at once drops it, and the
// accepting end holds the link until its keepalive closes it
// (e2e kcp-only-restart-relink, excess rows).
func TestKCPCloseFrameOnFreshLink(t *testing.T) {
	listener, err := kcpgo.ListenWithOptions("127.0.0.1:0", nil, 0, 0)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind dialer: %v", err)
	}
	t.Cleanup(func() { udp.Close() })
	sess, err := kcpgo.NewConn(listener.Addr().String(), nil, 0, 0, udp)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	outConn := kcpsrc.WrapKCPConn(sess, nil, nil, true, effectTimeout)

	// why: a request and an answer, then a last request with no answer, stand
	// in for the noise handshake and mux negotiation before a link exists.
	if _, err := outConn.Write([]byte("hello")); err != nil {
		t.Fatalf("dialer write: %v", err)
	}
	listener.SetDeadline(time.Now().Add(effectTimeout))
	accepted, err := listener.AcceptKCP()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	inConn := kcpsrc.WrapKCPConn(accepted, nil, nil, false, effectTimeout)
	readExactly(t, inConn, "hello", effectTimeout)
	if _, err := inConn.Write([]byte("hi")); err != nil {
		t.Fatalf("accepting end write: %v", err)
	}
	readExactly(t, outConn, "hi", effectTimeout)
	for i := 0; i < 4; i++ {
		if _, err := outConn.Write([]byte("mux2")); err != nil {
			t.Fatalf("dialer write: %v", err)
		}
	}

	mod := &Module{log: log.New(astral.Anyone)}
	outbound := newLink(mod, keepaliveConn{pipeIdentityConn{outConn}, quietKeepalive}, astral.NewNonce(), true)
	t.Cleanup(func() { outbound.Close() })
	if err := outbound.CloseWithError(nodesmod.ErrExcessLink); err != nil {
		t.Fatalf("CloseWithError: %v", err)
	}
	// why: the session lingers to deliver the frame, but the closing link ends at once.
	waitDone(t, outbound, absenceWindow, "closing end")

	// why: the close frame follows these writes on the stream, so a close that drops it drops them too.
	for i := 0; i < 4; i++ {
		readExactly(t, inConn, "mux2", peerCloseBound)
	}
	inbound := newLink(mod, keepaliveConn{pipeIdentityConn{inConn}, quietKeepalive}, astral.NewNonce(), false)
	t.Cleanup(func() { inbound.Close() })

	waitDone(t, inbound, peerCloseBound, "accepting end after the close frame on a fresh link")
	var closed *nodesmod.ErrPeerClosed
	if !errors.As(inbound.Err(), &closed) || closed.Reason != frames.CloseExcess {
		t.Fatalf("accepting end closed with %v; want ErrPeerClosed (excess)", inbound.Err())
	}
}

// readExactly reads len(want) bytes from conn within limit and fails the test unless they are want.
func readExactly(t *testing.T, conn net.Conn, want string, limit time.Duration) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(limit))
	defer conn.SetReadDeadline(time.Time{})
	buf := make([]byte, len(want))
	for n := 0; n < len(buf); {
		m, err := conn.Read(buf[n:])
		if err != nil {
			t.Fatalf("read %q within %v: %v", want, limit, err)
		}
		n += m
	}
	if string(buf) != want {
		t.Fatalf("read %q; want %q", buf, want)
	}
}
