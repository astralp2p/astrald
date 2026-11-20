package kcp

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

// TestLocalEndpointsAreWildcard records the known limit of waking links by
// local IP: the local endpoint of a kcp link, accepted or dialed, holds the
// wildcard address of its UDP socket and never the interface address, so no
// removed IP matches it.
// note: a fix, such as reading the destination address of each datagram,
// changes this result; the test then fails and the limit is lifted on purpose.
func TestLocalEndpointsAreWildcard(t *testing.T) {
	probe, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	listenPort := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()

	accepted := make(chan exonetmod.Conn, 1)
	handler := func(_ context.Context, conn exonetmod.Conn) (bool, error) {
		accepted <- conn
		return false, nil
	}

	srv := NewServer(&Module{log: log.New(astral.GenerateIdentity())}, astral.Uint16(listenPort), handler, 0)
	runServer(t, srv)

	remote := &kcp.Endpoint{IP: ip.IP(net.ParseIP("127.0.0.1")), Port: astral.Uint16(listenPort)}
	dialer := &Module{settings: Settings{Dial: &tree.Value[*astral.Bool]{}}}
	dialed, err := dialer.Dial(astral.NewContext(nil), remote)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer dialed.Close()

	// note: kcp sends nothing until the first write; the listener accepts on it.
	if _, err := dialed.Write([]byte{0}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var passive exonetmod.Conn
	select {
	case passive = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("server accepted no connection")
	}
	defer passive.Close()

	for name, conn := range map[string]exonetmod.Conn{"dialed": dialed, "accepted": passive} {
		local, ok := conn.LocalEndpoint().(*kcp.Endpoint)
		if !ok {
			t.Fatalf("%s: local endpoint is %T, want *kcp.Endpoint", name, conn.LocalEndpoint())
		}
		t.Logf("%s local endpoint: %v (unspecified: %v)", name, local.Address(), net.IP(local.IP).IsUnspecified())
		if !net.IP(local.IP).IsUnspecified() {
			t.Errorf("%s local endpoint %v is not a wildcard address; the known limit no longer holds", name, local.Address())
		}
	}
}
