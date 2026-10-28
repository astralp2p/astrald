package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeUseBlueprints grants reading and registering this node's blueprints
// to the user identity and to this node itself, and to nobody else.
//
// why the swarm is not in the grant: the action registers a blueprint, which
// fixes a type's wire shape for every caller of this node. A sibling reaches
// the blueprint ops through a node-local grant or a signed contract.
func (mod *Module) AuthorizeUseBlueprints(ctx *astral.Context, a *auth.UseBlueprintsAction) bool {
	return mod.authorizeUserOrNode(a.Actor())
}
