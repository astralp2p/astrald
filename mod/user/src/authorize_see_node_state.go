package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeSeeNodeState grants reads of this node's state to the user identity
// and to this node itself, and to nobody else.
//
// note: the state is tree values and listings, the directory's alias map and
// filters, agent metadata, and the log stream (auth.SeeNodeStateAction).
//
// why the swarm is not in the grant: the action covers the log stream, which
// records every caller's activity, and no feature needs a sibling to read it. A
// sibling reaches these ops through a node-local grant or a signed contract.
func (mod *Module) AuthorizeSeeNodeState(ctx *astral.Context, a *auth.SeeNodeStateAction) bool {
	return mod.authorizeUserOrNode(a.Actor())
}
