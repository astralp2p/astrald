package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeAdminNetwork grants network administration to the user identity, to
// this node itself, and to every current node member of this node's swarm, and
// to nobody else.
//
// why the user: the six other node-wide actions grant it, and a user who adopts
// and expels swarm members already reshapes the network more than these ops do.
// The end-to-end drivers dial links and punch holes under the user's token.
// why this node: the NAT link strategy and session migration query this node's
// own nat, kcp and nodes ops through the default client, which calls as this
// node (core/run.go).
// why before swarm membership: a node links and punches before any user exists,
// when LocalSwarm is empty and Identity() answers nil.
// why the swarm's node members: the NAT link strategy creates the ephemeral
// listener and the endpoint mapping on the peer, and the peer runs this rule.
// note: a network link or an app registration is not membership. LocalSwarm
// lists only subjects of the user's unexpelled swarm-membership contracts.
// note: a local caller carrying no identity also arrives as this node
// (core/router.go), so that branch is no proof that a call is internal.
// why a zero actor is refused first: the user identity is nil on an unclaimed
// node, and Identity.IsEqual reports a zero identity equal to nil.
func (mod *Module) AuthorizeAdminNetwork(ctx *astral.Context, a *auth.AdminNetworkAction) bool {
	actor := a.Actor()

	if actor.IsZero() {
		return false
	}

	if actor.IsEqual(mod.Identity()) || actor.IsEqual(mod.node.Identity()) {
		return true
	}

	for _, nodeID := range mod.LocalSwarm() {
		if nodeID.IsEqual(actor) {
			return true
		}
	}

	return false
}
