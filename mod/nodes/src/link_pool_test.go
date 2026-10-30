package nodes

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
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
