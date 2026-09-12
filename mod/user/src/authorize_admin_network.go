package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeAdminNetwork grants network administration to this node itself and
// to every current node member of this node's swarm, and to nobody else.
//
// why this node: the NAT link strategy and session migration query this node's
// own nat, kcp and nodes ops through the default client, which calls as this
// node (core/run.go).
// why before swarm membership: a node links and punches before any user exists,
// when LocalSwarm is empty.
// why the swarm's node members: the NAT link strategy creates the ephemeral
// listener and the endpoint mapping on the peer, and the peer runs this rule.
// note: a network link or an app registration is not membership. LocalSwarm
// lists only subjects of the user's unexpelled swarm-membership contracts.
// note: the user identity is not in the grant. It reaches these ops through a
// node-local grant or a signed contract, as any other identity does.
// note: a local caller carrying no identity also arrives as this node
// (core/router.go), so this branch is no proof that a call is internal.
func (mod *Module) AuthorizeAdminNetwork(ctx *astral.Context, a *auth.AdminNetworkAction) bool {
	if a.Actor().IsEqual(mod.node.Identity()) {
		return true
	}

	for _, nodeID := range mod.LocalSwarm() {
		if nodeID.IsEqual(a.Actor()) {
			return true
		}
	}

	return false
}
