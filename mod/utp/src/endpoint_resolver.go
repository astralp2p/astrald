package utp

import (
	"github.com/cryptopunkscc/astral-go/api/nodes"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/sig"
	nodesmod "github.com/cryptopunkscc/astrald/mod/nodes"
)

var _ nodesmod.EndpointResolver = &Module{}

// ResolveEndpoints returns the local node's own uTP endpoints; yields an empty
// channel for any other identity.
func (mod *Module) ResolveEndpoints(ctx *astral.Context, nodeID *astral.Identity) (_ <-chan *nodes.EndpointWithTTL, err error) {
	if !nodeID.IsEqual(mod.node.Identity()) {
		return sig.ArrayToChan([]*nodes.EndpointWithTTL{}), nil
	}

	return sig.ArrayToChan(mod.endpoints()), nil
}
