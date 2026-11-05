package nearby

import (
	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

var _ nodesmod.EndpointResolver = &Module{}

// The ports a nearby peer is dialed on: the tcp and kcp listen_port defaults.
//
// why: a status carries no endpoints, so nearby dials a peer at its datagram source IP
// on these fixed well-known ports; a node listening on non-default ports, or several
// nodes on one host, cannot be reached through nearby.
// todo: nearby may carry a port list later, not every node uses these ports; in stealth
// the list is encrypted under the node-derived key.
const (
	nearbyTCPPort astral.Uint16 = 1791
	nearbyKCPPort astral.Uint16 = 1792
)

// ResolveEndpoints returns a tcp and a kcp endpoint at the source IP of each verified
// cached status of nodeID, each valid for the rest of the entry's life.
func (mod *Module) ResolveEndpoints(ctx *astral.Context, nodeID *astral.Identity) (_ <-chan *nodes.EndpointWithTTL, err error) {
	now := mod.now()

	var list []*nodes.EndpointWithTTL
	for _, e := range mod.cache.live(now) {
		if !e.isNode(nodeID) {
			continue
		}

		ttl := e.remaining(now)
		list = append(list,
			nodes.NewEndpointWithTTL(&tcp.Endpoint{IP: e.IP, Port: nearbyTCPPort}, ttl),
			nodes.NewEndpointWithTTL(&kcp.Endpoint{IP: e.IP, Port: nearbyKCPPort}, ttl),
		)
	}

	return sig.ArrayToChan(list), nil
}
