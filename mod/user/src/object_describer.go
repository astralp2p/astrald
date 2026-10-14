package user

import (
	"sync"

	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/core"
)

// DescribeObject asks every other member of the local swarm to describe the
// object, and streams their descriptors. A peer describes an object without
// holding its bytes. Serves only the network zone; returns ErrZoneExcluded otherwise.
func (mod *Module) DescribeObject(ctx *astral.Context, objectID *astral.ObjectID) (<-chan *objects.Descriptor, error) {
	if !ctx.Zone().Is(astral.ZoneNetwork) {
		return nil, astral.ErrZoneExcluded
	}

	out := make(chan *objects.Descriptor)
	var wg sync.WaitGroup

	for _, peer := range mod.SwarmPeers() {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// why: the host authorized its caller; the peer serves the host as itself
			// and trusts that decision (objects.describe spec).
			// note: the context identity must be the node too, or the link relays for the caller (core/router_adapter.go).
			// why: the peer answers from its own describers only. A query arriving over a
			// link is marked origin=network (mod/nodes/src/mux.go), and objects.describe
			// drops the network zone for it, so the peer never asks back.
			descriptors, errPtr := objectscli.New(peer, core.NewClientAs(mod.node, mod.node.Identity())).Describe(ctx.WithIdentity(mod.node.Identity()), objectID)
			if descriptors == nil {
				if errPtr != nil && *errPtr != nil {
					mod.log.Errorv(1, "describe %v: %v", peer, *errPtr)
				}
				return
			}

			for {
				descriptor, ok, err := sig.RecvOk(ctx, descriptors)
				if err != nil {
					return
				}
				if !ok {
					if errPtr != nil && *errPtr != nil {
						mod.log.Errorv(1, "describe %v: %v", peer, *errPtr)
					}
					return
				}

				if err := sig.Send(ctx, out, descriptor); err != nil {
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
