package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeServeObjects allows the user (userOrSelf) to take every role:
// describer, finder, searcher and indexer. It refuses every other identity.
//
// why every role: each built-in action's handler names the user (operator
// decision, 2026-11-27).
// why a claimed node's own identity is not in the rule, unlike
// authorizeUserOrNode: a local caller carrying no identity is promoted to it
// (core/router.go), so granting the node would let any unauthenticated local
// process register. An unclaimed node is the user (userOrSelf).
// why: a zero actor is refused first. Identity.IsEqual reports a zero identity
// equal to nil.
// note: an app reaches any role through a node-local grant (mod/apphost) or a
// signed contract.
func (mod *Module) AuthorizeServeObjects(ctx *astral.Context, a *auth.ServeObjectsAction) bool {
	return !a.Actor().IsZero() && a.Actor().IsEqual(mod.userOrSelf())
}
