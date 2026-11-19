package nodes

import (
	"fmt"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"sync"

	"github.com/astralp2p/astral-go/api/nodes"
	nodescli "github.com/astralp2p/astral-go/api/nodes/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
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

// UpdateNodeEndpoints asks a remote resolver node for identity's endpoints,
// stores them locally, and returns how many of them were new.
// Per-endpoint store errors are logged, not returned.
func (mod *Module) UpdateNodeEndpoints(ctx *astral.Context, resolver *astral.Identity, identity *astral.Identity) (int, error) {
	endpoints, err := mod.resolveRemote(ctx.IncludeZone(astral.ZoneNetwork), resolver, identity)
	if err != nil {
		return 0, fmt.Errorf("resolve endpoints: %v", err)
	}

	return mod.storeEndpoints(identity, endpoints)
}

// resolveRemoteEndpoints calls nodes.resolve_endpoints on resolver for identity.
func resolveRemoteEndpoints(ctx *astral.Context, resolver *astral.Identity, identity *astral.Identity) ([]*nodes.EndpointWithTTL, error) {
	return nodescli.New(resolver, astrald.Default()).ResolveEndpoints(ctx, identity)
}

// storeEndpoints stores endpoints of identity and returns how many of them had
// no unexpired row before. A row that is only refreshed does not count.
func (mod *Module) storeEndpoints(identity *astral.Identity, endpoints []*nodes.EndpointWithTTL) (int, error) {
	rows, err := mod.db.FindEndpoints(identity)
	if err != nil {
		return 0, fmt.Errorf("find endpoints: %v", err)
	}

	known := make(map[endpointKey]bool, len(rows))
	for _, row := range rows {
		known[endpointKey{row.Network, row.Address}] = true
	}

	added := 0
	for _, ep := range endpoints {
		err = mod.AddEndpoint(identity, ep)
		if err != nil {
			mod.log.Error("adding resolved endpoint failed %v: %v", ep, err)
			continue
		}

		key := endpointKey{ep.Network(), ep.Address()}
		if !known[key] {
			known[key] = true
			added++
		}
	}

	return added, nil
}

// endpointKey names one endpoint row of an identity.
type endpointKey struct {
	network string
	address string
}
