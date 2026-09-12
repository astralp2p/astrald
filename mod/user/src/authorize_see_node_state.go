package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeSeeNodeState allows the user identity and this node's own identity to
// read the node's state, and nobody else.
//
// note: the state is tree values and listings, the directory's alias map and
// filters, agent metadata, and the log stream (auth.SeeNodeStateAction).
//
// why: the log stream carries every caller's logged activity, so the default
// holders are the owner and the node, as for AuthorizeAdminObjects.
// A swarm sibling or an app reaches these reads through a node-local grant or a
// signed contract.
func (mod *Module) AuthorizeSeeNodeState(ctx *astral.Context, a *auth.SeeNodeStateAction) bool {
	return mod.authorizeUserOrNode(a.Actor())
}
