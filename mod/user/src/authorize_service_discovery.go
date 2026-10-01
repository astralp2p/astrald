package user

import (
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeServiceDiscovery allows the user identity to discover every service,
// and refuses every other identity.
//
// why this node's own identity is not in the rule, unlike authorizeUserOrNode: a
// local caller carrying no identity is promoted to it (core/router.go), so
// granting the node would let any unauthenticated local process discover every
// service.
// why: a zero actor is refused first. The user identity is nil on an unclaimed
// node, and Identity.IsEqual reports a zero identity equal to nil.
// note: an app discovers a service through a node-local grant with a
// DiscoveryScope (mod/apphost) or a signed contract carrying one.
func (mod *Module) AuthorizeServiceDiscovery(ctx *astral.Context, a *services.ServiceDiscoveryAction) bool {
	if a.Actor().IsZero() {
		return false
	}
	return a.Actor().IsEqual(mod.Identity())
}
