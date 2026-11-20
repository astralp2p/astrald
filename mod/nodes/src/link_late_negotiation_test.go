package nodes

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/nodes/frames"
	"github.com/astralp2p/astrald/mod/nodes/src/noise"
)

// TestNegotiationPastContextSendsCloseFrame: when the dialer's context ends
// after the peer read the selected feature, the dialer still reads the link
// nonce the peer sends, so the peer that registered the link gets a close
// frame for it, and EstablishOutboundLink fails with the context's error.
//
// why: the basic strategy cancels its other rows once one row links; a row
// cut off after the peer sent its nonce left the peer a link the dialer never
// held (e2e kcp-only-restart-relink, excess rows).
func TestNegotiationPastContextSendsCloseFrame(t *testing.T) {
	localKey, peerKey := newNegotiationKey(t), newNegotiationKey(t)
	id := astral.IdentityFromPubKey(localKey.PubKey())
	mod := &Module{privateKey: &crypto.PrivateKey{Key: localKey.Serialize()}, log: log.New(id)}

	a, b := net.Pipe()
	deadline := time.Now().Add(negotiationTimeout)
	_ = a.SetDeadline(deadline)
	_ = b.SetDeadline(deadline)
	t.Cleanup(func() {
		_ = a.Close()
		_ = b.Close()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	peerGot := make(chan error, 1)
	nonce := astral.NewNonce()
	go func() {
		aconn, err := noise.HandshakeInbound(context.Background(), &negotiationPipeConn{Conn: b}, peerKey)
		if err != nil {
			peerGot <- fmt.Errorf("peer handshake: %w", err)
			return
		}
		ch := channel.New(aconn)
		steps := []peerStep{
			peerSend("feature count", astral.NewUint16(1)),
			peerSend("feature", astral.NewString8(featureMux2)),
			peerReceive("selected feature"),
			{"cancel the dialer's context", func(*channel.Channel) error { cancel(); return nil }},
			peerSend("negotiation status", astral.NewUint8(0)),
			peerSend("link nonce", &nonce),
		}
		for _, step := range steps {
			if err := step.run(ch); err != nil {
				peerGot <- fmt.Errorf("peer %s: %w", step.name, err)
				return
			}
		}
		obj, err := ch.Receive()
		if err != nil {
			peerGot <- fmt.Errorf("peer read after the nonce: %w", err)
			return
		}
		if _, ok := obj.(*frames.Close); !ok {
			peerGot <- fmt.Errorf("peer read %T after the nonce; want a close frame", obj)
			return
		}
		peerGot <- nil
	}()

	_, err := mod.EstablishOutboundLink(ctx, astral.IdentityFromPubKey(peerKey.PubKey()), &negotiationPipeConn{Conn: a, outbound: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("EstablishOutboundLink error %v; want one wrapping %v", err, context.Canceled)
	}
	if err := <-peerGot; err != nil {
		t.Fatalf("%v", err)
	}
}
