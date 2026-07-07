package nodes

import (
	"fmt"
	nodesmod "github.com/cryptopunkscc/astrald/mod/nodes"
	"sync"

	"github.com/cryptopunkscc/astral-go/api/nodes"
	nodescli "github.com/cryptopunkscc/astral-go/api/nodes/client"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
)

// ResolveEndpoints fans out to all registered resolvers and merges their results
// into one channel, closed once every resolver finishes.
func (mod *Module) ResolveEndpoints(ctx *astral.Context, nodeID *astral.Identity) (_ <-chan *nodes.EndpointWithTTL, err error) {
	var ch = make(chan *nodes.EndpointWithTTL)

	go func() {
		var wg sync.WaitGroup

		// spawn all resolvers
		for _, r := range mod.resolvers.Clone() {
			r := r
			wg.Add(1)
			go func() {
				defer wg.Done()
				mod.runResolver(ctx, r, nodeID, ch)
			}()
		}

		// wait for all resolvers to finish
		wg.Wait()
		close(ch)
	}()

	return ch, nil
}

func (mod *Module) runResolver(ctx *astral.Context, r nodesmod.EndpointResolver, nodeID *astral.Identity, out chan<- *nodes.EndpointWithTTL) {
	ch, err := r.ResolveEndpoints(ctx, nodeID)
	if err != nil {
		return
	}
	for {
		var e *nodes.EndpointWithTTL
		var ok bool

		// read the next endpoint from the resolver
		select {
		case e, ok = <-ch:
			if !ok {
				return
			}
		case <-ctx.Done():
			return
		}

		// write the endpoint upstream
		select {
		case out <- e:
		case <-ctx.Done():
			return
		}
	}
}

// UpdateNodeEndpoints asks a remote resolver node for identity's endpoints and
// stores them locally. Per-endpoint store errors are logged, not returned.
func (mod *Module) UpdateNodeEndpoints(ctx *astral.Context, resolver *astral.Identity, identity *astral.Identity) error {
	client := nodescli.New(resolver, astrald.Default())

	endpoints, err := client.ResolveEndpoints(ctx.IncludeZone(astral.ZoneNetwork), identity)
	if err != nil {
		return fmt.Errorf("resolve endpoints: %v", err)
	}

	for _, ep := range endpoints {
		err = mod.AddEndpoint(identity, ep)
		if err != nil {
			mod.log.Error("adding resolved endpoint failed %v: %v", ep, err)
		}
	}

	return nil
}
