package nodes

import (
	"fmt"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"sync"
	"time"

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
// A pull about the node's own identity queries nothing and stores nothing.
func (mod *Module) UpdateNodeEndpoints(ctx *astral.Context, resolver *astral.Identity, identity *astral.Identity) (int, error) {
	// why: a peer's reply about this node is a second-hand view of its listeners,
	// which DBEndpointResolver never serves for the node's own identity.
	if identity.IsEqual(mod.node.Identity()) {
		return 0, nil
	}

	endpoints, err := mod.resolveRemote(ctx.IncludeZone(astral.ZoneNetwork), resolver, identity)
	if err != nil {
		return 0, fmt.Errorf("resolve endpoints: %v", err)
	}

	return mod.storeEndpoints(identity, endpoints, resolver.IsEqual(identity))
}

// resolveRemoteEndpoints calls nodes.resolve_endpoints on resolver for identity.
func resolveRemoteEndpoints(ctx *astral.Context, resolver *astral.Identity, identity *astral.Identity) ([]*nodes.EndpointWithTTL, error) {
	return nodescli.New(resolver, astrald.Default()).ResolveEndpoints(ctx, identity)
}

// storeEndpoints stores endpoints of identity and returns how many of them had
// no unexpired row before. A row that is only refreshed does not count.
// A first-hand reply (authoritative) sets the expiry of the rows it lists and
// shortens the rows identity no longer lists; see shortenMissingEndpoints.
// It also counts the day for each row it lists; see recordEndpointSeen.
// A second-hand reply never shortens a row and counts no day; see StorePulledEndpoint.
func (mod *Module) storeEndpoints(identity *astral.Identity, endpoints []*nodes.EndpointWithTTL, authoritative bool) (int, error) {
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
		key := endpointKey{ep.Network(), ep.Address()}
		err = mod.db.StorePulledEndpoint(identity, key, endpointExpiry(ep), authoritative)
		if err != nil {
			mod.log.Error("adding resolved endpoint failed %v: %v", ep, err)
			continue
		}

		if authoritative {
			mod.recordEndpointSeen(identity, key)
		}

		if !known[key] {
			known[key] = true
			added++
		}
	}

	if authoritative {
		mod.shortenMissingEndpoints(identity, endpoints)
	}

	return added, nil
}

// recordEndpointSeen counts today as a day on which identity itself listed key.
// note: it only ranks the endpoint; no expiry depends on it.
func (mod *Module) recordEndpointSeen(identity *astral.Identity, key endpointKey) {
	if err := mod.db.RecordEndpointSeen(identity, key, time.Now()); err != nil {
		mod.log.Logv(1, "recording that %v lists %v %v: %v", identity, key.network, key.address, err)
	}
}

// missingEndpointTTL is the most a row its subject stopped listing has left.
// why: a reply raced with an address change can miss a good row, so the row
// lapses within an hour instead of at once; the next first-hand pull renews it.
const missingEndpointTTL = time.Hour

// shortenMissingEndpoints caps at missingEndpointTTL the expiry of every row of
// identity on a network endpoints lists whose address endpoints does not list.
// An empty reply and the networks it does not list change nothing.
// note: only the subject is authoritative; a second-hand reply never shortens.
func (mod *Module) shortenMissingEndpoints(identity *astral.Identity, endpoints []*nodes.EndpointWithTTL) {
	listed := map[string][]string{}
	for _, ep := range endpoints {
		listed[ep.Network()] = append(listed[ep.Network()], ep.Address())
	}

	until := time.Now().Add(missingEndpointTTL)
	for network, addresses := range listed {
		n, err := mod.db.ShortenEndpointExpiry(identity, network, addresses, until)
		if err != nil {
			mod.log.Error("shortening missing %v endpoints of %v: %v", network, identity, err)
			continue
		}
		if n > 0 {
			mod.log.Logv(1, "%v no longer lists %v %v endpoints, expiring them", identity, n, network)
		}
	}
}

// endpointKey names one endpoint row of an identity.
type endpointKey struct {
	network string
	address string
}
