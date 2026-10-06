package kcp

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	kcpgo "github.com/xtaci/kcp-go/v5"
)

// reserveUDPPort returns a loopback UDP port that was free a moment ago.
func reserveUDPPort(t *testing.T) int {
	t.Helper()

	probe, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer probe.Close()

	return probe.LocalAddr().(*net.UDPAddr).Port
}

// startRun runs srv and returns a channel that receives Run's result.
func startRun(t *testing.T, srv *Server) <-chan error {
	t.Helper()

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	result := make(chan error, 1)
	go func() { result <- srv.Run(ctx) }()

	return result
}

// TestServerCloseAfterRunClosesDone pins that Close fires Done once Run has
// bound its listener. Without it the watcher goroutine in Run outlives the
// server until its context ends.
func TestServerCloseAfterRunClosesDone(t *testing.T) {
	listenPort := reserveUDPPort(t)

	accepted := make(chan struct{}, 1)
	handler := func(context.Context, exonetmod.Conn) (bool, error) {
		accepted <- struct{}{}
		return false, nil
	}

	mod := &Module{log: log.New(astral.GenerateIdentity())}
	srv := NewServer(mod, astral.Uint16(listenPort), handler, 0)
	result := startRun(t, srv)

	// why: an accepted session proves Run has bound its listener, so Close
	// takes the listener path rather than the close-before-run path.
	client, err := kcpgo.DialWithOptions(net.JoinHostPort("127.0.0.1", strconv.Itoa(listenPort)), nil, 0, 0)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	if _, err := client.Write([]byte{0}); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("server accepted no connection")
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case <-srv.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done not closed after Close of a running server")
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Close")
	}
}

// TestServerCloseBeforeRunReleasesPort pins that a Run started after Close
// returns nil without holding the port, so an ephemeral listener closed right
// after it was created does not stay bound with no map entry to reclaim it.
func TestServerCloseBeforeRunReleasesPort(t *testing.T) {
	listenPort := reserveUDPPort(t)

	mod := &Module{log: log.New(astral.GenerateIdentity())}
	handler := func(context.Context, exonetmod.Conn) (bool, error) { return false, nil }
	srv := NewServer(mod, astral.Uint16(listenPort), handler, 0)

	if err := srv.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case err := <-startRun(t, srv):
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run after Close did not return")
	}

	rebind, err := net.ListenUDP("udp", &net.UDPAddr{Port: listenPort})
	if err != nil {
		t.Fatalf("port still bound after Run returned: %v", err)
	}
	rebind.Close()
}
