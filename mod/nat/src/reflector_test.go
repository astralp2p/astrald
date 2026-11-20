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

// TestPublicIPv4IgnoresNonGlobalRanges holds that a dialed link whose remote
// address no peer can reach is not a reflector, while a documentation range is.
func TestPublicIPv4IgnoresNonGlobalRanges(t *testing.T) {
	id := astral.GenerateIdentity()

	tests := []struct {
		addr string
		want string
	}{
		{"100.64.0.5:1791", ""},
		{"100.127.255.255:1791", ""},
		{"192.0.0.8:1791", ""},
		{"198.18.0.1:1791", ""},
		{"0.0.0.1:1791", ""},
		{"240.0.0.1:1791", ""},
		{"100.63.255.255:1791", "100.63.255.255"},
		{"31.61.247.94:1791", "31.61.247.94"},
		{"192.0.2.1:1791", "192.0.2.1"},
		{"198.51.100.1:1791", "198.51.100.1"},
		{"203.0.113.1:1791", "203.0.113.1"},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			got := publicIPv4(link(t, id, tt.addr))
			if tt.want == "" {
				if got != nil {
					t.Fatalf("publicIPv4(%s) = %v; want nil", tt.addr, got)
				}
				return
			}
			if got.String() != tt.want {
				t.Fatalf("publicIPv4(%s) = %v; want %s", tt.addr, got, tt.want)
			}
		})
	}
}

// TestPickReflectorSkipsCarrierInternalLink holds that a link to a CGNAT address
// never becomes the reflector, even for the punch peer.
func TestPickReflectorSkipsCarrierInternalLink(t *testing.T) {
	peer, other := astral.GenerateIdentity(), astral.GenerateIdentity()

	node, addr := pickReflector([]*nodes.LinkInfo{
		link(t, peer, "100.64.0.5:1791"),
		link(t, other, "45.59.119.20:1791"),
	}, peer)

	if node == nil || !node.IsEqual(other) || addr.String() != "45.59.119.20" {
		t.Fatalf("picked %v at %v; want %v at 45.59.119.20", node, addr, other)
	}
}
