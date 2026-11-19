package nat

import (
	"bytes"
	"net"
	"testing"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
)

// fakeEndpoint is a link endpoint with an arbitrary address; any method but
// Address panics on the embedded nil interface.
type fakeEndpoint struct {
	exonet.Endpoint
	addr string
}

func (e fakeEndpoint) Address() string { return e.addr }

// link is an outbound link to id that this node dialed at addr.
func link(t *testing.T, id *astral.Identity, addr string) *nodes.LinkInfo {
	t.Helper()
	l := &nodes.LinkInfo{RemoteIdentity: id, Outbound: true, RemoteEndpoint: fakeEndpoint{addr: addr}}
	if e, err := tcp.ParseEndpoint(addr); err == nil {
		l.RemoteEndpoint = e
	}
	return l
}

// inbound is a link id dialed, seen from addr.
func inbound(t *testing.T, id *astral.Identity, addr string) *nodes.LinkInfo {
	t.Helper()
	l := link(t, id, addr)
	l.Outbound = false
	return l
}

func TestPickReflector(t *testing.T) {
	peer, other, private := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()

	tests := []struct {
		name     string
		links    []*nodes.LinkInfo
		wantNode *astral.Identity
		wantAddr string
	}{
		{"the public punch peer first", []*nodes.LinkInfo{
			link(t, other, "45.59.119.20:1791"), link(t, peer, "45.59.119.13:1791"),
		}, peer, "45.59.119.13"},
		{"another public node when the peer is reached over tor", []*nodes.LinkInfo{
			link(t, peer, "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzab.onion:1791"),
			link(t, private, "10.77.0.3:1791"),
			link(t, other, "45.59.119.20:1791"),
		}, other, "45.59.119.20"},
		{"not a peer seen at a public address it does not listen on", []*nodes.LinkInfo{
			inbound(t, peer, "212.105.131.237:40213"),
		}, nil, ""},
		{"nobody with a public IPv4", []*nodes.LinkInfo{
			link(t, peer, "192.168.1.4:1791"), link(t, other, "[2001:db8::1]:1791"),
		}, nil, ""},
		{"no links", nil, nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, addr := pickReflector(tt.links, peer)
			switch {
			case tt.wantNode == nil && node != nil:
				t.Fatalf("picked %v at %v; want none", node, addr)
			case tt.wantNode != nil && (node == nil || !node.IsEqual(tt.wantNode)):
				t.Fatalf("picked %v; want %v", node, tt.wantNode)
			case tt.wantNode != nil && addr.String() != tt.wantAddr:
				t.Fatalf("picked address %v; want %v", addr, tt.wantAddr)
			}
		})
	}
}

// The token leaves from the punch socket itself, reflectTries times.
func TestSendFromSocket_SendsFromThePunchSocket(t *testing.T) {
	conn := &scriptedConn{}
	p := openScripted(t, conn)
	token := bytes.Repeat([]byte{0x7e}, 16)

	if err := p.sendFromSocket(token, &net.UDPAddr{IP: net.ParseIP("45.59.119.20"), Port: 47001}, reflectTries); err != nil {
		t.Fatalf("sendFromSocket: %v", err)
	}
	if all, withToken := conn.writesTo("45.59.119.20:47001", token); all != reflectTries || withToken != reflectTries {
		t.Fatalf("wrote %d datagrams (%d with the token); want %d", all, withToken, reflectTries)
	}
}
