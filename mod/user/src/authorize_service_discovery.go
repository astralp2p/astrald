package user

import (
	"slices"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeServiceDiscovery allows the user identity to discover every service,
// and another member of the local swarm to discover every service on this node.
// It refuses every other identity.
//
// why the swarm member rule: a node carries an app's discovery to the swarm in
// the app's name; what the app may reach is checked against the app's own
// grant on its node before the discovery is carried.
// why this node's own identity is not in the rule, unlike authorizeUserOrNode: a
// local caller carrying no identity is promoted to it (core/router.go), so
// granting the node would let any unauthenticated local process discover every
// service.
// why: a zero actor is refused first. The user identity is nil on an unclaimed
// node, and Identity.IsEqual reports a zero identity equal to nil.
// note: an app discovers a service through a node-local grant with a
// DiscoveryScope (mod/apphost) or a signed contract carrying one.
func (mod *Module) AuthorizeServiceDiscovery(ctx *astral.Context, a *services.ServiceDiscoveryAction) bool {
	actor := a.Actor()
	switch {
	case actor.IsZero():
		return false
	case actor.IsEqual(mod.Identity()):
		return true
	case actor.IsEqual(mod.node.Identity()):
		return false
	}
	return a.NodeID.IsEqual(mod.node.Identity()) && slices.ContainsFunc(mod.LocalSwarm(), actor.IsEqual)
}
