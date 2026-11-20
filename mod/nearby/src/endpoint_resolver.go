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

// ResolveEndpoints returns a tcp and a kcp endpoint at the source IP of each verified
// cached status of nodeID, on the ports its status carried or the defaults 1791 and 1792,
// each valid for the rest of the entry's life. A port of 0 gives no endpoint.
//
// Nodes on one host broadcast from one IP and hold one entry each, keyed by IP and tcp port,
// so each resolves to its own ports.
//
// Spec: .ai/system/protocols/nearby/README.md
func (mod *Module) ResolveEndpoints(ctx *astral.Context, nodeID *astral.Identity) (_ <-chan *nodes.EndpointWithTTL, err error) {
	now := mod.now()

	var list []*nodes.EndpointWithTTL
	for _, e := range mod.cache.live(now) {
		if !e.isNode(nodeID) {
			continue
		}

		ttl := e.remaining(now)
		tcpPort, kcpPort := e.listenPorts()
		if tcpPort != 0 {
			list = append(list, nodes.NewEndpointWithTTL(&tcp.Endpoint{IP: e.IP, Port: tcpPort}, ttl))
		}
		if kcpPort != 0 {
			list = append(list, nodes.NewEndpointWithTTL(&kcp.Endpoint{IP: e.IP, Port: kcpPort}, ttl))
		}
	}

	return sig.ArrayToChan(list), nil
}
