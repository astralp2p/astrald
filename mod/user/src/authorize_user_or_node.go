package user

import "github.com/astralp2p/astral-go/astral"

// authorizeUserOrNode allows the user identity and this node's own identity, and
// nobody else.
//
// why: the rule AuthorizeAdminObjects applies, shared by the node-wide actions
// that grant no sibling by default. This node's own identity is in the rule
// because a local caller carrying no identity is promoted to it (core/router.go),
// which is how the CLI and the setup ceremony reach these ops. Any other identity
// reaches them through a node-local grant or a signed contract.
func (mod *Module) authorizeUserOrNode(actor *astral.Identity) bool {
	return actor.IsEqual(mod.Identity()) || actor.IsEqual(mod.node.Identity())
}
