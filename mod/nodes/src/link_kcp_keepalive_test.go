package nodes

import (
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	kcpsrc "github.com/astralp2p/astrald/mod/kcp/src"
	kcpgo "github.com/xtaci/kcp-go/v5"
)

// mutedPacketConn is a UDP socket that, once muted, drops every packet in
// both directions, as the socket of a frozen or vanished peer would.
type mutedPacketConn struct {
	net.PacketConn
	muted atomic.Bool
}

func (c *mutedPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if c.muted.Load() {
		return len(p), nil
	}
	return c.PacketConn.WriteTo(p, addr)
}

func (c *mutedPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		n, addr, err := c.PacketConn.ReadFrom(p)
		if err != nil || !c.muted.Load() {
			return n, addr, err
		}
	}
}

// kcpLinkPair is a link over a real KCP session on loopback UDP.
type kcpLinkPair struct {
	inbound  *Link            // the accepting end
	outbound *Link            // the dialing end
	dialer   *mutedPacketConn // the dialing end's UDP socket
}

// newKCPLinkPair dials a KCP listener on loopback, runs a link with
// keepalive k on each end, and returns once a ping from the dialer was answered.
func newKCPLinkPair(t *testing.T, k exonetmod.LinkKeepalive) kcpLinkPair {
	t.Helper()
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
	dialer := &mutedPacketConn{PacketConn: udp}
	sess, err := kcpgo.NewConn(listener.Addr().String(), nil, 0, 0, dialer)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	mod := &Module{log: log.New(astral.Anyone)}
	// why effectTimeout: the kcp module wraps each session with its dial
	// timeout; the first frame read clears it, as in production.
	outConn := kcpsrc.WrapKCPConn(sess, nil, nil, true, effectTimeout)
	outbound := newLink(mod, keepaliveConn{pipeIdentityConn{outConn}, k}, astral.NewNonce(), true)
	t.Cleanup(func() { outbound.Close() })

	// why: the listener accepts the session at its first packet, which this ping sends.
	pinged := make(chan error, 1)
	go func() { _, err := outbound.Ping(); pinged <- err }()
	listener.SetDeadline(time.Now().Add(effectTimeout))
	accepted, err := listener.AcceptKCP()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	inConn := kcpsrc.WrapKCPConn(accepted, nil, nil, false, effectTimeout)
	inbound := newLink(mod, keepaliveConn{pipeIdentityConn{inConn}, k}, astral.NewNonce(), false)
	t.Cleanup(func() { inbound.Close() })

	if err := <-pinged; err != nil {
		t.Fatalf("the dialer's first ping failed: %v", err)
	}
	return kcpLinkPair{inbound: inbound, outbound: outbound, dialer: dialer}
}

// TestPassiveKCPLinkClosesOnSilentPeer: the accepting end of a KCP link whose
// dialer goes silent without closing (its socket drops every packet) is
// closed by its own keepalive, idle plus timeout after the silence, and no
// later than the start jitter plus idle plus timeout.
//
// note: kcp-go marks a session dead after 20 retransmissions but never acts
// on it, and the dial timeout ended at the first read, so nothing below the
// link closes this session; the "ping timeout" error pins the keepalive as the cause.
func TestPassiveKCPLinkClosesOnSilentPeer(t *testing.T) {
	const idle, timeout = 400 * time.Millisecond, 400 * time.Millisecond
	pair := newKCPLinkPair(t, exonetmod.LinkKeepalive{Idle: idle, Timeout: timeout})

	// why: the pong is a frame the accepting end counts as hearing from its
	// peer, so its idle interval starts at the silence.
	if _, err := pair.inbound.Ping(); err != nil {
		t.Fatalf("the accepting end's ping failed: %v", err)
	}
	pair.dialer.muted.Store(true)
	start := time.Now()

	select {
	case <-pair.inbound.Done():
	case <-time.After(effectTimeout):
		t.Fatalf("the accepting end kept the link to a silent dialer for %v", effectTimeout)
	}
	took := time.Since(start)
	if limit := pingJitter + idle + timeout + 200*time.Millisecond; took > limit {
		t.Fatalf("the accepting end closed the link %v after the silence; want at most %v", took, limit)
	}
	if floor := idle + timeout - 50*time.Millisecond; took < floor {
		t.Fatalf("the accepting end closed the link %v after the silence; want at least %v", took, floor)
	}
	if err := pair.inbound.Err(); err == nil || !strings.Contains(err.Error(), "ping timeout") {
		t.Fatalf("the accepting end closed the link with %v; want ping timeout", err)
	}
	t.Logf("the accepting end closed the link %v after the silence", took)
}
