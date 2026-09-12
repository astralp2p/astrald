package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeAdminManageApps grants the administration of app and agent
// credentials to the user identity and to this node itself, and to nobody else.
//
// why: the six ops it covers had no authorization at all, so there is no policy
// to carry over. Listing is in the grant because a list hands out the bearer
// credentials it enumerates.
// why not the local swarm: a token minted here authenticates as any identity it
// names, so a sibling holding this action could act on this node as the user.
func (mod *Module) AuthorizeAdminManageApps(ctx *astral.Context, a *auth.AdminManageAppsAction) bool {
	return mod.authorizeUserOrNode(a.Actor())
}
