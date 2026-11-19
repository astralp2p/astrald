package gateway

import (
	nodesmod "github.com/astralp2p/astrald/mod/nodes"

	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

var _ nodesmod.EndpointResolver = &Module{}

// ResolveEndpoints returns gateway-wrapped endpoints only for this node's own
// identity, one per gateway it holds a registration with; requests for any
// other node return an empty result immediately.
// note: could be extended to resolve endpoints for nodes this module gateways for.
func (mod *Module) ResolveEndpoints(context *astral.Context, nodeID *astral.Identity) (<-chan *nodes.EndpointWithTTL, error) {
	if !nodeID.IsEqual(mod.node.Identity()) {
		// note: we might resolve endpoints if we act as their gateway
		return sig.ArrayToChan([]*nodes.EndpointWithTTL{}), nil
	}

	var endpoints []*nodes.EndpointWithTTL
	for _, gw := range mod.gateways.Clone() {
		// why: a gw row for a gateway without a registration is a dial that cannot succeed.
		if _, ok := mod.registered.Get(gw.String()); !ok {
			continue
		}
		endpoints = append(endpoints, nodes.NewEndpointWithTTL(gateway.NewEndpoint(gw, mod.node.Identity()), endpointTTL))
	}

	return sig.ArrayToChan(endpoints), nil
}
