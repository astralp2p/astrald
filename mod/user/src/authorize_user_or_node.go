package user

import "github.com/astralp2p/astral-go/astral"

// userOrSelf answers the identity that holds the user's powers on this node:
// the user identity once the node is claimed, and this node's own identity
// while it is unclaimed.
//
// why: an unclaimed node acts as the user (operator decision, 2026-11-27).
// Every handler that names the user names it through userOrSelf, so an
// unclaimed node holds the user's powers for itself, and a contract it signs
// grants them to an app through the auth walk. A claimed node's own identity
// holds none of them through this rule.
//
// why rootKey: the swarm's entry rules answer the same identity through
// rootKey (swarm_wire.go), so the handlers and the entry rules cannot disagree
// on who the user is.
// note: Identity reads the stored active contract, which LoadDependencies
// binds before it registers any handler.
func (mod *Module) userOrSelf() *astral.Identity {
	return mod.rootKey()
}

// authorizeUserOrNode allows the user (userOrSelf) and this node's own
// identity, and nobody else.
//
// why: the rule AuthorizeAdminObjects applies, shared by the node-wide actions
// that grant no sibling by default. This node's own identity is in the rule
// because a session with the node's token acts as it, which is how the CLI and
// the step-by-step setup reach these ops; a token-less local caller is promoted
// to it too (core/router.go), within apphost's gate. Any other
// identity reaches them through a node-local grant or a signed contract.
//
// why: a zero actor is refused first. Identity.IsEqual reports a zero identity
// equal to nil.
func (mod *Module) authorizeUserOrNode(actor *astral.Identity) bool {
	if actor.IsZero() {
		return false
	}
	return actor.IsEqual(mod.userOrSelf()) || actor.IsEqual(mod.node.Identity())
}
