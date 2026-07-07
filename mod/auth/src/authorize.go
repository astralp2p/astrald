package auth

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/auth"
)

// Authorize checks whether the action is permitted: directly by a registered
// handler, or through a chain of contracts delegating the action from an
// identity a handler allows down to the actor. Each chain link must carry a
// matching permit whose Delegation covers the number of hops below the link
// (the link closest to the actor needs none) and whose constraints pass, so
// authority attenuates and its flow is bounded by every issuer on the path.
func (mod *Module) Authorize(ctx *astral.Context, action auth.ActionObject) bool {
	visited := map[string]struct{}{action.Actor().String(): {}}
	return mod.authorize(ctx, action, 0, visited)
}

// authorize runs one step of the chain walk. hopsBelow is the number of chain
// links between action's current actor and the original caller; visited holds
// identities already on the path (cycle guard).
func (mod *Module) authorize(ctx *astral.Context, action auth.ActionObject, hopsBelow int, visited map[string]struct{}) bool {
	actionType := action.ObjectType()
	actor := action.Actor()

	if mod.authorizeHandlers(ctx, action) {
		if hopsBelow == 0 {
			mod.log.Logv(1, "allow %v %v", actor, actionType)
		}
		return true
	}

	// todo: maybe we can specify max depth of delegation

	contracts, err := mod.SignedContracts().WithSubject(actor).WithAction(action).Find(ctx)
	if err != nil {
		mod.log.Logv(1, "error finding active contracts: %v", err)
		return false
	}

	for _, sc := range contracts {
		if _, seen := visited[sc.Issuer.String()]; seen {
			continue
		}

		for _, p := range sc.HasPermit(actionType) {
			// the link must permit the delegation that happened below it
			if int(p.Delegation) < hopsBelow {
				continue
			}
			// constraints apply at every link, so a chain can only narrow
			if !p.Allows(action) {
				continue
			}

			// re-enter as the issuer; restore the actor on the way out
			visited[sc.Issuer.String()] = struct{}{}
			action.SetActor(sc.Issuer)
			allowed := mod.authorize(ctx, action, hopsBelow+1, visited)
			action.SetActor(actor)
			delete(visited, sc.Issuer.String())

			if allowed {
				if hopsBelow == 0 {
					mod.log.Logv(1, "allow %v %v (delegated by %v)", actor, actionType, sc.Issuer)
				}
				return true
			}
		}
	}

	return false
}

func (mod *Module) authorizeHandlers(ctx *astral.Context, action auth.ActionObject) bool {
	for _, h := range mod.get(action.ObjectType()) {
		if h.Authorize(ctx, action) {
			return true
		}
	}
	return false
}
