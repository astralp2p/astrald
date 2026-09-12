package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeServeApps allows the user identity and this node's own identity to
// host on this node: install an app handler and publish a service advertisement.
//
// why: hosting is a place on the node, so the rule is authorizeUserOrNode's and
// grants no sibling by default.
// note: an app reaches ServeApps through the node-local grant apphost.register
// writes for it (mod/apphost), or through a signed contract.
func (mod *Module) AuthorizeServeApps(ctx *astral.Context, a *auth.ServeAppsAction) bool {
	return mod.authorizeUserOrNode(a.Actor())
}
