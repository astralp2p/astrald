package user

import (
	"slices"
	"sync"

	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/core"
)

// SearchObject asks every other member of the local swarm to search, and
// streams their results. Serves only the network zone; returns ErrZoneExcluded otherwise.
func (mod *Module) SearchObject(ctx *astral.Context, query objects.SearchQuery) (<-chan *objects.SearchResult, error) {
	if !ctx.Zone().Is(astral.ZoneNetwork) {
		return nil, astral.ErrZoneExcluded
	}

	out := make(chan *objects.SearchResult)
	var wg sync.WaitGroup

	for _, peer := range slices.DeleteFunc(mod.LocalSwarm(), mod.node.Identity().IsEqual) {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// why: the host authorized its caller; the peer serves the host as itself
			// and trusts that decision (objects.search spec).
			// note: the context identity must be the node too, or the link relays for the caller (core/router_adapter.go).
			// why: the peer answers from its own searchers only. A query arriving over a
			// link is marked origin=network (mod/nodes/src/mux.go), and objects.search
			// drops the network zone for it, so the peer never asks back.
			results, errPtr := objectscli.New(peer, core.NewClientAs(mod.node, mod.node.Identity())).Search(ctx.WithIdentity(mod.node.Identity()), query)
			if results == nil {
				if errPtr != nil && *errPtr != nil {
					mod.log.Errorv(1, "search %v: %v", peer, *errPtr)
				}
				return
			}

			for {
				result, ok, err := sig.RecvOk(ctx, results)
				if err != nil {
					return
				}
				if !ok {
					if errPtr != nil && *errPtr != nil {
						mod.log.Errorv(1, "search %v: %v", peer, *errPtr)
					}
					return
				}

				if err := sig.Send(ctx, out, result); err != nil {
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out, nil
}
