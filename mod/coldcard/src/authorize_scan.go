package coldcard

import (
	"github.com/astralp2p/astral-go/api/coldcard"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeScanAction allows this node itself and the swarm's user to scan the
// node's attached Coldcard devices, and nobody else.
//
// why the rule lives here: this module owns the action, so it owns the rule that
// answers it.
// why this node: a scan runs during the setup ceremony on an unclaimed node,
// where a local caller carrying no identity arrives as this node
// (core/router.go).
// why the user module is optional: a node that loads no user module still scans
// as itself, and Identity() has no answer before a user claims the node.
// note: a node-local grant (mod/apphost) and a signed contract are additional
// paths to the action. This rule answers for neither.
// note: a zero actor is refused first, because Identity.IsEqual reports a zero
// identity equal to the nil identity an unclaimed node answers.
func (mod *Module) AuthorizeScanAction(_ *astral.Context, a *coldcard.ScanAction) bool {
	actor := a.Actor()

	if actor.IsZero() {
		return false
	}

	if actor.IsEqual(mod.node.Identity()) {
		return true
	}

	return mod.User != nil && actor.IsEqual(mod.User.Identity())
}
