package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeSeeNodeState allows the user identity, this node's own identity, and
// every current node member of this node's swarm to read the node's state, and
// nobody else.
//
// note: the state is tree values and listings, the directory's alias map and
// filters, agent metadata, and the log stream (auth.SeeNodeStateAction).
//
// note: the swarm's node members are admitted so the user's own nodes read each
// other's state.
// note: the action covers the log stream, so a node member reads this node's
// logged activity for every caller.
// note: the swarm's grant stops at reading. AuthorizeConfigureNodeState admits
// no sibling, so a sibling changes nothing without a node-local grant or a
// signed contract.
// todo: the rule's original reason was the remote tree mount, removed with
// mod/tree.MountRemote. Whether the swarm clause still earns its breadth is
// undecided.
// why a zero actor is refused first: a contract subject can be nil, and
// Identity.IsEqual reports a zero identity equal to nil.
// note: LocalSwarm lists only subjects of the user's unexpelled
// swarm-membership contracts. A link or an app registration is not membership.
func (mod *Module) AuthorizeSeeNodeState(ctx *astral.Context, a *auth.SeeNodeStateAction) bool {
	actor := a.Actor()

	if actor.IsZero() {
		return false
	}

	if mod.authorizeUserOrNode(actor) {
		return true
	}

	for _, nodeID := range mod.LocalSwarm() {
		if nodeID.IsEqual(actor) {
			return true
		}
	}

	return false
}
