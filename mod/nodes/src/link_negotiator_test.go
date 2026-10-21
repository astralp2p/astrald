package nodes

import (
	"context"
	"fmt"
	"net"
	"runtime/debug"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	exonetapi "github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astrald/mod/exonet"
	"github.com/astralp2p/astrald/mod/nodes/src/noise"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// why: the deadline turns a stuck handshake or negotiation into a test failure instead of a hang.
const negotiationTimeout = 10 * time.Second

type negotiationPipeConn struct {
	net.Conn
	outbound bool
}

func (c *negotiationPipeConn) Outbound() bool                     { return c.outbound }
func (c *negotiationPipeConn) LocalEndpoint() exonetapi.Endpoint  { return nil }
func (c *negotiationPipeConn) RemoteEndpoint() exonetapi.Endpoint { return nil }

// peerStep is one negotiation step of the remote peer over its side of the noise conn.
type peerStep struct {
	name string
	run  func(ch *channel.Channel) error
}

func peerSend(name string, obj astral.Object) peerStep {
	return peerStep{name, func(ch *channel.Channel) error { return ch.Send(obj) }}
}

func peerReceive(name string) peerStep {
	return peerStep{name, func(ch *channel.Channel) error {
		_, err := ch.Receive()
		return err
	}}
}

func newNegotiationKey(t *testing.T) *secp256k1.PrivateKey {
	t.Helper()
	key, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}
	return key
}

// runClosingPeer completes the noise handshake as the remote peer, runs steps, and closes the conn.
func runClosingPeer(conn *negotiationPipeConn, handshake func(exonet.Conn) (*noise.Conn, error), steps []peerStep) <-chan error {
	done := make(chan error, 1)
	go func() {
		defer conn.Close()
		aconn, err := handshake(conn)
		if err != nil {
			done <- fmt.Errorf("peer handshake: %w", err)
			return
		}
		ch := channel.New(aconn)
		for _, step := range steps {
			if err := step.run(ch); err != nil {
				done <- fmt.Errorf("peer %s: %w", step.name, err)
				return
			}
		}
		done <- nil
	}()
	return done
}

// callRecovered runs fn and returns its error, or the panic value and stack when fn panics.
//
// why: a panic would end the test binary at the first case instead of failing that case.
func callRecovered(fn func() error) (err error, panicked string) {
	defer func() {
		if r := recover(); r != nil {
			panicked = fmt.Sprintf("%v\n%s", r, debug.Stack())
		}
	}()
	return fn(), ""
}

// establishAgainstClosingPeer runs establish against a peer that closes after the handshake and
// after each of peerScript's steps; establish must return an error and must not panic.
func establishAgainstClosingPeer(
	t *testing.T,
	peerScript []peerStep,
	establish func(mod *Module, peerKey *secp256k1.PrivateKey, conn exonet.Conn) error,
	peerHandshake func(localKey, peerKey *secp256k1.PrivateKey, conn exonet.Conn) (*noise.Conn, error),
) {
	for n := 0; n <= len(peerScript); n++ {
		name := "after handshake"
		if n > 0 {
			name = "after " + peerScript[n-1].name
		}
		t.Run(name, func(t *testing.T) {
			localKey, peerKey := newNegotiationKey(t), newNegotiationKey(t)
			mod := &Module{privateKey: &crypto.PrivateKey{Key: localKey.Serialize()}}

			a, b := net.Pipe()
			deadline := time.Now().Add(negotiationTimeout)
			_ = a.SetDeadline(deadline)
			_ = b.SetDeadline(deadline)
			t.Cleanup(func() {
				_ = a.Close()
				_ = b.Close()
			})
			localConn := &negotiationPipeConn{Conn: a}
			peerConn := &negotiationPipeConn{Conn: b}

			peerDone := runClosingPeer(peerConn, func(conn exonet.Conn) (*noise.Conn, error) {
				return peerHandshake(localKey, peerKey, conn)
			}, peerScript[:n])

			err, panicked := callRecovered(func() error { return establish(mod, peerKey, localConn) })
			if panicked != "" {
				t.Fatalf("establish panicked: %v", panicked)
			}
			if err == nil {
				t.Fatal("establish returned nil; want an error from a peer that closed mid-negotiation")
			}
			if peerErr := <-peerDone; peerErr != nil {
				t.Fatalf("%v", peerErr)
			}
		})
	}
}

func TestEstablishInboundLinkFailsOnPeerClosingMidNegotiation(t *testing.T) {
	// note: the outbound peer's side of NegotiateInbound, in protocol order.
	script := []peerStep{
		peerReceive("feature count"),
		peerReceive("feature"),
		peerSend("selected feature", astral.NewString8(featureMux2)),
		peerReceive("negotiation status"),
	}
	establishAgainstClosingPeer(t, script,
		func(mod *Module, _ *secp256k1.PrivateKey, conn exonet.Conn) error {
			return mod.EstablishInboundLink(context.Background(), conn)
		},
		func(localKey, peerKey *secp256k1.PrivateKey, conn exonet.Conn) (*noise.Conn, error) {
			return noise.HandshakeOutbound(context.Background(), conn, localKey.PubKey(), peerKey)
		},
	)
}

func TestEstablishOutboundLinkFailsOnPeerClosingMidNegotiation(t *testing.T) {
	// note: the inbound peer's side of NegotiateOutbound, in protocol order.
	script := []peerStep{
		peerSend("feature count", astral.NewUint16(1)),
		peerSend("feature", astral.NewString8(featureMux2)),
		peerReceive("selected feature"),
		peerSend("negotiation status", astral.NewUint8(0)),
	}
	establishAgainstClosingPeer(t, script,
		func(mod *Module, peerKey *secp256k1.PrivateKey, conn exonet.Conn) error {
			_, err := mod.EstablishOutboundLink(context.Background(), astral.IdentityFromPubKey(peerKey.PubKey()), conn)
			return err
		},
		func(_, peerKey *secp256k1.PrivateKey, conn exonet.Conn) (*noise.Conn, error) {
			return noise.HandshakeInbound(context.Background(), conn, peerKey)
		},
	)
}
