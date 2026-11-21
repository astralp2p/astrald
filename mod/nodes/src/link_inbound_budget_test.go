package nodes

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	exonetapi "github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/nodes/src/noise"
)

// endpointPipeConn is a pipe conn whose local endpoint is on network, as a transport's inbound conn reports it.
type endpointPipeConn struct {
	negotiationPipeConn
	network string
}

func (c *endpointPipeConn) LocalEndpoint() exonetapi.Endpoint {
	return netEndpoint{network: c.network}
}

// TestEstablishInboundLinkEndsAbandonedNegotiationAtRowBudget: a dialer that completes the
// handshake and then sends nothing, as a kcp dialer that abandoned its row, ends the
// inbound negotiation at the row budget of the network, and the peer's inbound
// negotiations go idle, so awaitRivals stops waiting for it.
func TestEstablishInboundLinkEndsAbandonedNegotiationAtRowBudget(t *testing.T) {
	const budget = 300 * time.Millisecond
	setProfile(t, "kcp", TransportProfile{Rank: 40, IdlePing: kcpIdlePing, PingTimeout: profilePingTimeout, RowBudget: budget})

	localKey, peerKey := newNegotiationKey(t), newNegotiationKey(t)
	mod := &Module{privateKey: &crypto.PrivateKey{Key: localKey.Serialize()}}
	peer := astral.IdentityFromPubKey(peerKey.PubKey())

	a, b := net.Pipe()
	deadline := time.Now().Add(negotiationTimeout)
	_ = a.SetDeadline(deadline)
	_ = b.SetDeadline(deadline)
	t.Cleanup(func() {
		_ = a.Close()
		_ = b.Close()
	})
	localConn := &endpointPipeConn{negotiationPipeConn: negotiationPipeConn{Conn: a}, network: "kcp"}
	peerConn := &negotiationPipeConn{Conn: b, outbound: true}

	// note: the peer completes the handshake, then selects no feature and reads until closed.
	peerDone := make(chan error, 1)
	go func() {
		aconn, err := noise.HandshakeOutbound(context.Background(), peerConn, localKey.PubKey(), peerKey)
		if err != nil {
			peerDone <- fmt.Errorf("peer handshake: %w", err)
			return
		}
		buf := make([]byte, 64)
		for {
			if _, err := aconn.Read(buf); err != nil {
				peerDone <- nil
				return
			}
		}
	}()

	start := time.Now()
	err := mod.EstablishInboundLink(context.Background(), localConn)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("EstablishInboundLink registered a link from a peer that never selected a feature")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EstablishInboundLink error %v after %v; want one wrapping %v", err, elapsed, context.DeadlineExceeded)
	}
	if elapsed > budget+2*time.Second {
		t.Fatalf("EstablishInboundLink returned after %v; want it ended at the %v kcp row budget", elapsed, budget)
	}
	select {
	case <-mod.inboundNegotiations.idle(peer):
	default:
		t.Fatal("the peer's inbound negotiations are not idle after the negotiation ended")
	}
	if peerErr := <-peerDone; peerErr != nil {
		t.Fatalf("%v", peerErr)
	}
}

// TestInboundBudgetIsTheRowBudget: an inbound conn runs under the row budget of its network:
// the shipped kcp 5 s and gw 20 s; none on tcp, tor, or a conn with no endpoint.
func TestInboundBudgetIsTheRowBudget(t *testing.T) {
	want := map[string]time.Duration{"kcp": 5 * time.Second, "gw": 20 * time.Second, "tcp": 0, "tor": 0}
	for network, budget := range want {
		conn := &endpointPipeConn{network: network}
		if got := inboundBudget(conn); got != budget {
			t.Fatalf("inboundBudget(%v conn) = %v; want %v", network, got, budget)
		}
	}
	if got := inboundBudget(&negotiationPipeConn{}); got != 0 {
		t.Fatalf("inboundBudget(conn with no endpoint) = %v; want 0", got)
	}
}
