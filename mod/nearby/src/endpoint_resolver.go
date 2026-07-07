package nearby

import (
	"github.com/cryptopunkscc/astral-go/api/nodes"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	nodesmod "github.com/cryptopunkscc/astrald/mod/nodes"
)

var _ nodesmod.EndpointResolver = &Module{}

// ResolveEndpoints returns endpoints for a node by scanning cached nearby status messages
// for EndpointWithTTL attachments that match the given identity.
func (mod *Module) ResolveEndpoints(ctx *astral.Context, nodeID *astral.Identity) (_ <-chan *nodes.EndpointWithTTL, err error) {
	var list []*nodes.EndpointWithTTL

	for _, v := range mod.Cache().Clone() {
		if v.GetIdentity() == nil {
			continue
		}

		if !v.GetIdentity().IsEqual(nodeID) {
			continue
		}

		endpoints := astral.SelectByType[*nodes.EndpointWithTTL](v.Status.Attachments.Objects())
		if len(endpoints) > 0 {
			list = append(list, endpoints...)
			continue
		}
	}

	return sig.ArrayToChan(list), nil
}
