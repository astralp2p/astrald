package gateway

import (
	"time"

	"github.com/cryptopunkscc/astral-go/api/gateway"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/mod/nodes"
)

var _ nodes.EndpointResolver = &Module{}

// ResolveEndpoints returns gateway-wrapped endpoints only for this node's own
// identity; requests for any other node return an empty result immediately.
// note: could be extended to resolve endpoints for nodes this module gateways for.
func (mod *Module) ResolveEndpoints(context *astral.Context, nodeID *astral.Identity) (<-chan *nodes.EndpointWithTTL, error) {
	if !nodeID.IsEqual(mod.node.Identity()) {
		// note: we might resolve endpoints if we act as their gateway
		return sig.ArrayToChan([]*nodes.EndpointWithTTL{}), nil
	}

	var endpoints []*nodes.EndpointWithTTL
	for _, gw := range mod.gateways.Clone() {
		endpoints = append(endpoints, nodes.NewEndpointWithTTL(gateway.NewEndpoint(gw, mod.node.Identity()), 7*30*24*time.Hour))
	}

	return sig.ArrayToChan(endpoints), nil
}
