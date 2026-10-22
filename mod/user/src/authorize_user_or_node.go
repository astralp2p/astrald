package user

import "github.com/astralp2p/astral-go/astral"

// authorizeUserOrNode allows the user identity and this node's own identity, and
// nobody else.
//
// why: the rule AuthorizeAdminObjects applies, shared by the node-wide actions
// that grant no sibling by default. This node's own identity is in the rule
// because a session with the node's token acts as it, which is how the CLI and
// the step-by-step setup reach these ops; a token-less local caller is promoted
// to it too (core/router.go), within apphost's anonymous_allowlist. Any other
// identity reaches them through a node-local grant or a signed contract.
//
// why: a zero actor is refused first. The user identity is nil on an unclaimed
// node, and Identity.IsEqual reports a zero identity equal to nil.
func (mod *Module) authorizeUserOrNode(actor *astral.Identity) bool {
	if actor.IsZero() {
		return false
	}
	return actor.IsEqual(mod.Identity()) || actor.IsEqual(mod.node.Identity())
}
