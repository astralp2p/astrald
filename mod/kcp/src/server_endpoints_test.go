package kcp

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	kcpgo "github.com/xtaci/kcp-go/v5"
)

// An accepted conn reports the dialing peer as its remote endpoint and the
// listener as its local endpoint, so link records name the right peer.
func TestServerAcceptedConnEndpoints(t *testing.T) {
	// why: Run binds ":port" and exposes no way to read the bound port back, so
	// the test reserves a free loopback port first.
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

	mod := &Module{log: log.New(astral.GenerateIdentity())}
	srv := NewServer(mod, astral.Uint16(listenPort), handler, 0)
	runServer(t, srv)

	client, err := kcpgo.DialWithOptions(net.JoinHostPort("127.0.0.1", strconv.Itoa(listenPort)), nil, 0, 0)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	clientPort := client.LocalAddr().(*net.UDPAddr).Port

	// note: kcp retransmits the write until it is acked, so it reaches the
	// listener even if Run has not bound the port yet.
	if _, err := client.Write([]byte{0}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var conn exonetmod.Conn
	select {
	case conn = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("server accepted no connection")
	}

	remote, ok := conn.RemoteEndpoint().(*kcp.Endpoint)
	if !ok {
		t.Fatalf("remote endpoint is %T, want *kcp.Endpoint", conn.RemoteEndpoint())
	}
	local, ok := conn.LocalEndpoint().(*kcp.Endpoint)
	if !ok {
		t.Fatalf("local endpoint is %T, want *kcp.Endpoint", conn.LocalEndpoint())
	}

	if int(remote.Port) != clientPort {
		t.Errorf("RemoteEndpoint port = %v, want client port %v", remote.Port, clientPort)
	}
	if int(local.Port) != listenPort {
		t.Errorf("LocalEndpoint port = %v, want listener port %v", local.Port, listenPort)
	}
}
