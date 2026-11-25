package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeAdminNetwork grants network administration to the user identity and
// to this node itself; a Privileged node holds it through the swarm root
// handler (authmod.SwarmWalk).
//
// why the user: the six other node-wide actions grant it, and a user who adopts
// and expels swarm members already reshapes the network more than these ops do.
// The end-to-end drivers dial links and punch holes under the user's token.
// why this node: the NAT link strategy and session migration query this node's
// own nat, kcp and nodes ops through the default client, which calls as this
// node (core/run.go).
// why before swarm membership: a node links and punches before any user exists,
// when LocalSwarm is empty and Identity() answers nil.
// note: the NAT link strategy creates the ephemeral listener and the endpoint
// mapping on the peer, which the peer allows a Privileged node alone.
// why not outsiders: the endpoints this node advertises, and the rows it holds
// about other nodes, are private to the user's swarm, and nodes.resolve_endpoints
// answers under this action.
// note: a user-signed contract that delegates this action to an outsider admits
// it to every network-admin op, not only to endpoint reads.
// note: the outsider paths are in .ai/system/topics/node-exposure.md#endpoint-exchange.
// note: a local caller carrying no identity also arrives as this node
// (core/router.go), so that branch is no proof that a call is internal.
// why a zero actor is refused first: the user identity is nil on an unclaimed
// node, and Identity.IsEqual reports a zero identity equal to nil.
func (mod *Module) AuthorizeAdminNetwork(ctx *astral.Context, a *auth.AdminNetworkAction) bool {
	actor := a.Actor()

	if actor.IsZero() {
		return false
	}

	// why no swarm member: a Privileged node acts for the user through the
	// swarm root handler, and no other tier administers this node.
	return actor.IsEqual(mod.Identity()) || actor.IsEqual(mod.node.Identity())
}
