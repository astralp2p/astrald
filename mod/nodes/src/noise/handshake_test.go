package noise

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/astral"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// why: the guard only catches a hang, and a cold handshake under -race took 0.35s on a Raspberry Pi 4.
const handshakeTimeout = 10 * time.Second

type pipeConn struct {
	net.Conn
	outbound bool
}

func (c *pipeConn) Outbound() bool                  { return c.outbound }
func (c *pipeConn) LocalEndpoint() exonet.Endpoint  { return nil }
func (c *pipeConn) RemoteEndpoint() exonet.Endpoint { return nil }

type handshakeResult struct {
	conn *Conn
	err  error
}

// why: a pipe deadline turns a stuck handshake into a test failure instead of a hang.
func newPipeConns(t *testing.T) (outbound, inbound *pipeConn) {
	t.Helper()
	a, b := net.Pipe()
	deadline := time.Now().Add(handshakeTimeout)
	_ = a.SetDeadline(deadline)
	_ = b.SetDeadline(deadline)
	t.Cleanup(func() {
		_ = a.Close()
		_ = b.Close()
	})
	return &pipeConn{Conn: a, outbound: true}, &pipeConn{Conn: b}
}

func newKey(t *testing.T) *secp256k1.PrivateKey {
	t.Helper()
	key, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}
	return key
}

func startInbound(ctx context.Context, conn *pipeConn, key *secp256k1.PrivateKey) <-chan handshakeResult {
	ch := make(chan handshakeResult, 1)
	go func() {
		c, err := HandshakeInbound(ctx, conn, key)
		ch <- handshakeResult{c, err}
	}()
	return ch
}

func awaitInbound(t *testing.T, ch <-chan handshakeResult) handshakeResult {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(handshakeTimeout):
		t.Fatalf("HandshakeInbound still running after %v", handshakeTimeout)
		return handshakeResult{}
	}
}

func TestHandshakeAuthenticatesBothSides(t *testing.T) {
	kA, kB := newKey(t), newKey(t)
	idA, idB := astral.IdentityFromPubKey(kA.PubKey()), astral.IdentityFromPubKey(kB.PubKey())
	outConn, inConn := newPipeConns(t)

	inbound := startInbound(context.Background(), inConn, kB)
	out, err := HandshakeOutbound(context.Background(), outConn, kB.PubKey(), kA)
	if err != nil {
		t.Fatalf("HandshakeOutbound: %v", err)
	}
	res := awaitInbound(t, inbound)
	if res.err != nil {
		t.Fatalf("HandshakeInbound: %v", res.err)
	}
	in := res.conn

	if got := out.RemoteIdentity(); !got.IsEqual(idB) {
		t.Errorf("outbound RemoteIdentity() = %v; want %v", got, idB)
	}
	if got := out.LocalIdentity(); !got.IsEqual(idA) {
		t.Errorf("outbound LocalIdentity() = %v; want %v", got, idA)
	}
	if got := in.RemoteIdentity(); !got.IsEqual(idA) {
		t.Errorf("inbound RemoteIdentity() = %v; want %v", got, idA)
	}
	if !out.Outbound() {
		t.Error("outbound Outbound() = false; want true")
	}

	go func() { _, _ = out.Write([]byte("hello")) }()

	p := make([]byte, 16)
	n, err := in.Read(p)
	if err != nil || string(p[:n]) != "hello" {
		t.Fatalf("inbound Read = (%q, %v); want (\"hello\", nil)", p[:n], err)
	}
}

func TestHandshakeOutboundRefusesSelf(t *testing.T) {
	kA := newKey(t)
	outConn, _ := newPipeConns(t)

	_, err := HandshakeOutbound(context.Background(), outConn, kA.PubKey(), kA)
	if err == nil || err.Error() != "local and remote identities cannot be equal" {
		t.Fatalf("HandshakeOutbound to self = %v; want \"local and remote identities cannot be equal\"", err)
	}
}

func TestHandshakeFailsOnWrongRemoteKey(t *testing.T) {
	kA, kB, kC := newKey(t), newKey(t), newKey(t)
	outConn, inConn := newPipeConns(t)

	inbound := startInbound(context.Background(), inConn, kB)
	_, outErr := HandshakeOutbound(context.Background(), outConn, kC.PubKey(), kA)
	res := awaitInbound(t, inbound)

	if outErr == nil {
		t.Error("HandshakeOutbound expecting another key = nil error; want an error")
	}
	if res.err == nil {
		t.Error("HandshakeInbound with a mismatched initiator = nil error; want an error")
	}
}

func TestHandshakeInboundHonorsCancel(t *testing.T) {
	_, inConn := newPipeConns(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(20*time.Millisecond, cancel)

	res := awaitInbound(t, startInbound(ctx, inConn, newKey(t)))
	if !errors.Is(res.err, context.Canceled) {
		t.Fatalf("HandshakeInbound after cancel = %v; want %v", res.err, context.Canceled)
	}
}
