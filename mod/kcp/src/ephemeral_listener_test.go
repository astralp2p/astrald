package kcp

import (
	"context"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

// testServer builds a KCP server on an OS-chosen port. The handler is never
// reached: the idle timeout depends on whether a connection arrived, not on what
// the handler does with one.
//
// note: the logger is real because Run logs before it accepts, and a nil
// *log.Logger has no nil guard.
func testServer(t *testing.T, idleTimeout time.Duration) *Server {
	t.Helper()

	mod := &Module{log: log.New(astral.GenerateIdentity())}
	handler := func(context.Context, exonetmod.Conn) (bool, error) { return false, nil }

	return NewServer(mod, 0, handler, idleTimeout)
}

// runServer starts srv and returns a channel closed when Run returns.
func runServer(t *testing.T, srv *Server) <-chan struct{} {
	t.Helper()

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = srv.Run(ctx)
	}()

	return stopped
}

// TestUnusedEphemeralListenerClosesItself pins the reclaim. A NAT traversal that
// fails after the peer created the listener never connects to it, and without
// this the port stays bound until the node restarts.
func TestUnusedEphemeralListenerClosesItself(t *testing.T) {
	srv := testServer(t, 50*time.Millisecond)
	stopped := runServer(t, srv)

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("an unused ephemeral listener stayed open past its idle timeout")
	}
}

// TestUsedEphemeralListenerSurvivesItsIdleTimeout pins the exemption. Close
// closes the UDP socket the accepted sessions read and write through, so
// reclaiming a used listener would kill every live link on the port.
func TestUsedEphemeralListenerSurvivesItsIdleTimeout(t *testing.T) {
	srv := testServer(t, 50*time.Millisecond)
	srv.accepted.Store(true)

	stopped := runServer(t, srv)

	select {
	case <-stopped:
		t.Fatal("closed a listener that had accepted a connection")
	case <-time.After(500 * time.Millisecond):
	}
}

// TestZeroIdleTimeoutKeepsTheListenerOpen covers the node's own listener, which
// startServer builds with no timeout and which must never be reclaimed.
func TestZeroIdleTimeoutKeepsTheListenerOpen(t *testing.T) {
	srv := testServer(t, 0)
	stopped := runServer(t, srv)

	select {
	case <-stopped:
		t.Fatal("closed a listener that carries no idle timeout")
	case <-time.After(500 * time.Millisecond):
	}
}
