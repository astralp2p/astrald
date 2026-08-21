package tor

import (
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

var _ nodesmod.EndpointResolver = &Module{}

// ResolveEndpoints returns the local node's onion endpoint with a ~90-day TTL.
// Returns an empty channel for any identity other than the local node, or when listening is disabled or the server has no endpoint yet.
func (mod *Module) ResolveEndpoints(ctx *astral.Context, nodeID *astral.Identity) (out <-chan *nodes.EndpointWithTTL, err error) {
	out = sig.ArrayToChan([]*nodes.EndpointWithTTL{})

	listen := mod.settings.Listen.Get()
	if listen != nil && !*listen {
		return sig.ArrayToChan([]*nodes.EndpointWithTTL{}), nil
	}

	switch {
	case !nodeID.IsEqual(mod.node.Identity()):
		return
	case mod.torServer == nil:
		return
	case mod.torServer.endpoint.IsZero():
		return
	}

	return sig.ArrayToChan([]*nodes.EndpointWithTTL{
		nodes.NewEndpointWithTTL(mod.torServer.endpoint, 3*30*24*time.Hour),
	}), nil
}
