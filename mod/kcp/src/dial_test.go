package kcp

import (
	"net"
	"testing"

	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// TestDialedConnCloseReleasesLocalPort pins that a dialed conn owns the UDP
// socket Dial created. With a local port pinned for the NAT hole handoff, a
// Close that leaves the socket open makes every later Dial to that endpoint
// fail with EADDRINUSE.
func TestDialedConnCloseReleasesLocalPort(t *testing.T) {
	// why: the pinned port must be free, so the test reserves one and releases it.
	probe, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	localPort := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()

	mod := &Module{settings: Settings{Dial: &tree.Value[*astral.Bool]{}}}

	// note: no peer listens here; NewConn only binds locally and sends nothing
	// until the conn is written to.
	remote, err := kcp.ParseEndpoint("127.0.0.1:9")
	if err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	if err := mod.SetEndpointLocalSocket(*remote, astral.Uint16(localPort), false); err != nil {
		t.Fatalf("pin local port: %v", err)
	}

	ctx := astral.NewContext(nil)

	first, err := mod.Dial(ctx, remote)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second, err := mod.Dial(ctx, remote)
	if err != nil {
		t.Fatalf("second dial after close: %v", err)
	}
	defer second.Close()

	local, ok := second.LocalEndpoint().(*kcp.Endpoint)
	if !ok {
		t.Fatalf("local endpoint is %T, want *kcp.Endpoint", second.LocalEndpoint())
	}
	if int(local.Port) != localPort {
		t.Errorf("second dial bound port %v, want pinned port %v", local.Port, localPort)
	}
}
