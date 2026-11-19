package nat

import (
	"net"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nat"
	natclient "github.com/astralp2p/astral-go/api/nat/client"
	"github.com/astralp2p/astral-go/api/nodes"
	nodesclient "github.com/astralp2p/astral-go/api/nodes/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
)

// reflectTries is how many times the token leaves the punch socket.
// why: UDP may drop one; the reflector answers the first that arrives.
const reflectTries = 3

// udpReflector reflects a puncher's socket off one linked node.
type udpReflector struct {
	puncher *conePuncher
	node    *astral.Identity
	addr    net.IP // the node's IPv4, from its link's remote endpoint
}

var _ nat.Reflector = &udpReflector{}

// Reflect opens nat.reflect_udp on the node, sends the token from the punch
// socket, and returns the endpoint the node saw.
func (r *udpReflector) Reflect(ctx *astral.Context) (*nat.Endpoint, error) {
	sess, err := natclient.New(r.node, astrald.Default()).ReflectUDP(ctx)
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	if err := r.puncher.sendFromSocket(sess.Token, &net.UDPAddr{IP: r.addr, Port: sess.Port}, reflectTries); err != nil {
		return nil, err
	}
	return sess.Seen(ctx)
}

// reflectorFor picks the node to reflect a punch with peer off, or returns nil
// when this node dialed no linked node at a public IPv4.
func (mod *Module) reflectorFor(ctx *astral.Context, puncher *conePuncher, peer *astral.Identity) nat.Reflector {
	links, err := nodesclient.New(nil, astrald.Default()).Links(ctx)
	if err != nil {
		mod.log.Logv(1, "no reflector for the punch with %v: %v", peer, err)
		return nil
	}

	node, addr := pickReflector(links, peer)
	if node == nil {
		mod.log.Log("no node dialed at a public IPv4 to reflect the punch with %v off", peer)
		return nil
	}

	mod.log.Log("reflecting the punch with %v off %v at %v", peer, node, addr)
	return &udpReflector{puncher: puncher, node: node, addr: addr}
}

// pickReflector returns peer when this node dialed it at a public IPv4, else the
// first other node this node dialed at one, with that IPv4.
//
// why the punch peer first: it costs no third node, and the endpoint it sees is
// the one the punch uses.
// why dialed links only: an inbound link's remote endpoint is the address the
// peer was seen from, which a NAT'd peer does not listen on.
func pickReflector(links []*nodes.LinkInfo, peer *astral.Identity) (*astral.Identity, net.IP) {
	var other *astral.Identity
	var otherAddr net.IP
	for _, l := range links {
		addr := publicIPv4(l)
		if addr == nil {
			continue
		}
		if l.RemoteIdentity.IsEqual(peer) {
			return l.RemoteIdentity, addr
		}
		if other == nil {
			other, otherAddr = l.RemoteIdentity, addr
		}
	}
	return other, otherAddr
}

// publicIPv4 returns the public IPv4 an outbound link's remote endpoint names, or nil.
func publicIPv4(l *nodes.LinkInfo) net.IP {
	if l == nil || !l.Outbound || l.RemoteEndpoint == nil || l.RemoteIdentity == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(l.RemoteEndpoint.Address())
	if err != nil {
		return nil
	}
	addr := net.ParseIP(host).To4()
	if addr == nil || !ip.IP(addr).IsPublic() {
		return nil
	}
	return addr
}
