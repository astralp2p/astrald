package tcp

import (
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

var _ nodesmod.EndpointResolver = &Module{}

// ResolveEndpoints returns the module's advertised TCP endpoints only when nodeID matches the local node;
// all other identities receive an empty channel.
func (mod *Module) ResolveEndpoints(ctx *astral.Context, nodeID *astral.Identity) (_ <-chan *nodes.EndpointWithTTL, err error) {
	if !nodeID.IsEqual(mod.node.Identity()) {
		return sig.ArrayToChan([]*nodes.EndpointWithTTL{}), nil
	}

	listen := mod.settings.Listen.Get()
	if listen != nil && !*listen {
		return sig.ArrayToChan([]*nodes.EndpointWithTTL{}), nil
	}

	return sig.ArrayToChan(mod.endpoints()), nil
}
