package nodes

import (
	nodesmod "github.com/cryptopunkscc/astrald/mod/nodes"
	"time"

	"github.com/cryptopunkscc/astral-go/api/nodes"
	"github.com/cryptopunkscc/astral-go/astral"
)

var _ nodesmod.EndpointResolver = &DBEndpointResolver{}

type DBEndpointResolver struct {
	mod *Module
}

// ResolveEndpoints is a nodes.EndpointResolver that fetches endpoints from the local database
func (r *DBEndpointResolver) ResolveEndpoints(ctx *astral.Context, nodeID *astral.Identity) (<-chan *nodes.EndpointWithTTL, error) {
	var ch = make(chan *nodes.EndpointWithTTL)

	rows, err := r.mod.db.FindEndpoints(nodeID)
	if err != nil {
		return nil, err
	}

	go func() {
		defer close(ch)

		for _, row := range rows {
			// parse the next row
			e, err := r.mod.Exonet.Parse(row.Network, row.Address)
			if err != nil {
				r.mod.log.Errorv(1, "DB.ResolveEndpoints: error parsing db row: %v", err)
				continue
			}

			// send the next endpoint unless the context has been canceled
			var re *nodes.EndpointWithTTL
			if row.ExpiresAt != nil {
				remaining := time.Until(*row.ExpiresAt)
				if remaining > 0 {
					re = nodes.NewEndpointWithTTL(e, remaining)
				} else {
					continue
				}
			} else {
				re = nodes.NewEndpointWithTTL(e)
			}

			select {
			case ch <- re:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

func (r *DBEndpointResolver) String() string { return "DBEndpointResolver" }
