package gateway

import (
	"strings"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
)

// parseGatewayEndpoints parses the configured endpoints of one gateway.
// An endpoint that does not parse is logged and skipped.
func (mod *Module) parseGatewayEndpoints(entry GatewayEntry) []exonet.Endpoint {
	var endpoints []exonet.Endpoint
	for _, s := range entry.Endpoints {
		network, address, ok := strings.Cut(s, ":")
		if !ok {
			mod.log.Error("gateway %v: endpoint %v is not network:address", entry.ID, s)
			continue
		}

		endpoint, err := mod.Exonet.Parse(network, address)
		if err != nil {
			mod.log.Error("gateway %v: endpoint %v: %v", entry.ID, s, err)
			continue
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints
}

// storeGatewayEndpoints stores the gateway's configured endpoints with a fresh TTL,
// so a registration query to the gateway has a route.
func (mod *Module) storeGatewayEndpoints(gatewayID *astral.Identity, endpoints []exonet.Endpoint) {
	for _, endpoint := range endpoints {
		if err := mod.Nodes.AddEndpoint(gatewayID, nodes.NewEndpointWithTTL(endpoint, endpointTTL)); err != nil {
			mod.log.Error("gateway %v: store endpoint %v: %v", gatewayID, endpoint, err)
		}
	}
}
