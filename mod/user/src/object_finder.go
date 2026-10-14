package user

import (
	"slices"
	"sync"
	"time"

	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/core"
)

// FindObject asks every other member of the local swarm whether its main
// repository holds the object, and streams those that answer true.
// Serves only the network zone; returns ErrZoneExcluded otherwise.
func (mod *Module) FindObject(ctx *astral.Context, id *astral.ObjectID) (<-chan *astral.Identity, error) {
	if !ctx.Zone().Is(astral.ZoneNetwork) {
		return nil, astral.ErrZoneExcluded
	}

	out := make(chan *astral.Identity)
	var wg sync.WaitGroup

	for _, peer := range slices.DeleteFunc(mod.LocalSwarm(), mod.node.Identity().IsEqual) {
		wg.Add(1)
		go func() {
			defer wg.Done()

			askCtx, cancel := ctx.WithTimeout(10 * time.Second)
			defer cancel()

			// why: the host authorized its caller; the peer serves the host as itself
			// and trusts that decision (objects.find spec).
			// note: the context identity must be the node too, or the link relays for the caller (core/router_adapter.go).
			has, err := objectscli.New(peer, core.NewClientAs(mod.node, mod.node.Identity())).Contains(askCtx.WithIdentity(mod.node.Identity()), "main", id)
			if err != nil {
				mod.log.Errorv(1, "find %v on %v: %v", id, peer, err)
				return
			}

			if has {
				sig.Send(ctx, out, peer)
			}
		}()
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out, nil
}
