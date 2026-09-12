package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeConfigureNodeState grants changes to this node's tree, its mounts,
// and its alias table to the user identity and to this node itself, and to
// nobody else.
//
// why: the five ops this action covers made no authorization call before, so no
// policy carries over. The rule is authorizeUserOrNode's.
//
// why the swarm is not in the grant: swarm membership is granted on request, and
// the tree holds every module's configuration. A sibling reaches these ops through
// a node-local grant or a signed contract.
func (mod *Module) AuthorizeConfigureNodeState(ctx *astral.Context, a *auth.ConfigureNodeStateAction) bool {
	return mod.authorizeUserOrNode(a.Actor())
}
