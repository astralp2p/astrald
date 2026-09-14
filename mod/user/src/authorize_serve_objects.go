package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeServeObjects allows the user identity to register as an indexer, and
// refuses every other role and every other identity.
//
// why: an indexer receives the ID of every object added to or removed from an
// indexed repository, so the default holder is the user alone.
// why this node's own identity is not in the rule, unlike authorizeUserOrNode: a
// local caller carrying no identity is promoted to it (core/router.go), so
// granting the node would let any unauthenticated local process register.
// why: a zero actor is refused first. The user identity is nil on an unclaimed
// node, and Identity.IsEqual reports a zero identity equal to nil.
// note: the describer, finder and searcher roles have no default holder. An app
// reaches any role through a node-local grant (mod/apphost) or a signed contract.
func (mod *Module) AuthorizeServeObjects(ctx *astral.Context, a *auth.ServeObjectsAction) bool {
	if a.Role != auth.RoleIndexer || a.Actor().IsZero() {
		return false
	}
	return a.Actor().IsEqual(mod.Identity())
}
