package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// nodeLocal is a handler whose authority answers for this node alone. The auth
// module marks one with NodeLocal.
type nodeLocal interface{ NodeLocal() }

// Authorize checks whether the action is permitted: directly by a registered
// handler, or through a chain of contracts delegating the action from an
// identity a handler allows down to the actor. Each chain link must carry a
// matching permit whose Delegation covers the number of hops below the link
// (the link closest to the actor needs none) and whose constraints pass, so
// authority attenuates and its flow is bounded by every issuer on the path.
// A handler the auth module marks NodeLocal answers for the caller alone, so no
// chain ends at one. A veto (AddVeto) refuses an identity at every level it
// names, before anything else is asked.
//
// note: the walk holds no identity above the others. The user's powers, and
// what a Privileged node may do, are what their handlers allow.
// Spec: .ai/system/protocols/auth/README.md#authority-in-a-swarm
func (mod *Module) Authorize(ctx *astral.Context, action auth.ActionObject) bool {
	// why: the anonymous identity is not a party, so nothing grants it an action.
	// note: a contract lookup for a zero subject drops its subject filter.
	if action.Actor().IsZero() {
		return false
	}

	visited := map[string]struct{}{action.Actor().String(): {}}
	return mod.authorize(ctx, action, 0, visited)
}

// authorize runs one step of the chain walk. hopsBelow is the number of chain
// links between action's current actor and the original caller; visited holds
// identities already on the path (cycle guard).
func (mod *Module) authorize(ctx *astral.Context, action auth.ActionObject, hopsBelow int, visited map[string]struct{}) bool {
	actionType := action.ObjectType()
	actor := action.Actor()

	// why first, at every level: a veto cuts an identity off whatever permits it
	// holds, so no handler, contract or authority is asked about it. Running at
	// each level denies a delegation chain through a vetoed issuer, and the
	// subject of a contract the veto names; the caller's other contracts still
	// get their turn.
	if mod.vetoed(actor) {
		if hopsBelow == 0 {
			mod.log.Logv(1, "deny %v %v (vetoed)", actor, actionType)
		}
		return false
	}

	if mod.authorizeHandlers(ctx, action, hopsBelow) {
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
		// why the issuer: a contract's authority is its two signatures, so the
		// walk continues at the identity that signed it as issuer, never at a
		// node that answered its request.
		next := sc.Issuer
		if _, seen := visited[next.String()]; seen {
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

			// re-enter as the next link; restore the actor on the way out
			visited[next.String()] = struct{}{}
			action.SetActor(next)
			allowed := mod.authorize(ctx, action, hopsBelow+1, visited)
			action.SetActor(actor)
			delete(visited, next.String())

			if allowed {
				if hopsBelow == 0 {
					mod.log.Logv(1, "allow %v %v (delegated by %v)", actor, actionType, sc.Issuer)
				}
				return true
			}
		}
	}

	// An external authority is the last word at this level: the actor's own
	// assignment when one names the action type, otherwise the action-wide one.
	// It answers for the actor this level names, so authority delegates through
	// it exactly as it does through a handler: a contract whose issuer the
	// authority permits carries its subject.
	//
	// why last: a handler and a contract both answer without leaving the host,
	// so neither should wait behind a question that does.
	//
	// note: a chain of depth N therefore puts N+1 questions to the authority,
	// one per level. Nothing remembers an answer yet.
	if ext, ok := mod.externalFor(actor, actionType); ok && ext.Authorize(ctx, action) {
		if hopsBelow == 0 {
			mod.log.Logv(1, "allow %v %v (external)", actor, actionType)
		}
		return true
	}

	return false
}

// authorizeHandlers asks the handlers registered for the action's type.
// hopsBelow is the number of chain links between the action's current actor and
// the original caller.
//
// why: a node-local handler answers only at hopsBelow 0. Its authority is a
// record this node keeps and no other node reads, so the holder has nothing to
// hand on; letting a chain link reach it would extend that record a hop.
func (mod *Module) authorizeHandlers(ctx *astral.Context, action auth.ActionObject, hopsBelow int) bool {
	for _, h := range mod.get(action.ObjectType()) {
		if _, local := h.(nodeLocal); local && hopsBelow > 0 {
			continue
		}
		if h.Authorize(ctx, action) {
			return true
		}
	}
	return false
}
