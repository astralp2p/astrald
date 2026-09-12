package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/coldcard"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeServeObjects answers whether this node has granted the actor the role
// it is asking to serve.
//
// why: a typed shim over the generic lookup. The auth registry dispatches on the
// concrete action type, so every grantable action needs one of these — the shim
// carries the type, authorizeGrant carries the decision.
func (mod *Module) AuthorizeServeObjects(ctx *astral.Context, action *auth.ServeObjectsAction) bool {
	return mod.authorizeGrant(ctx, action)
}

// AuthorizeSeeSwarm answers whether this node has granted the actor the right to
// read the swarm's state.
//
// why: a typed shim over the generic lookup, matching AuthorizeServeObjects. The
// auth registry dispatches on the concrete action type, so every grantable
// action needs one of these.
func (mod *Module) AuthorizeSeeSwarm(ctx *astral.Context, action *user.SeeSwarmAction) bool {
	return mod.authorizeGrant(ctx, action)
}

// AuthorizeAdminSwarm answers whether this node has granted the actor the right
// to change what the swarm holds.
func (mod *Module) AuthorizeAdminSwarm(ctx *astral.Context, action *user.AdminSwarmAction) bool {
	return mod.authorizeGrant(ctx, action)
}

// authorizeGrant answers from this node's grants alone: it looks up the actor's
// permit for the action being asked and lets the permit decide.
//
// why: no action-specific logic. Narrowing lives in the permit's constraints, so
// one function serves every grantable action — a ServeObjects grant limited to
// "describer" refuses "searcher" here without this code knowing what a role is.
//
// why: a lookup error refuses. A grant reaches ops that hand out standing
// authority, so a database fault must not read as permission.
func (mod *Module) authorizeGrant(_ *astral.Context, action auth.ActionObject) bool {
	permit, err := mod.db.FindGrant(action.Actor(), action.ObjectType())
	switch {
	case err != nil:
		mod.log.Errorv(1, "grant lookup for %v: %v", action.Actor(), err)
		return false
	case permit == nil:
		return false
	}

	return permit.Allows(action)
}

// AuthorizeAdminManageApps answers whether this node has granted the actor the
// right to administer app and agent credentials.
func (mod *Module) AuthorizeAdminManageApps(ctx *astral.Context, action *auth.AdminManageAppsAction) bool {
	return mod.authorizeGrant(ctx, action)
}

// AuthorizeConfigureNodeState answers whether this node has granted the actor
// the right to change this node's tree, its mounts, and its alias table.
func (mod *Module) AuthorizeConfigureNodeState(ctx *astral.Context, action *auth.ConfigureNodeStateAction) bool {
	return mod.authorizeGrant(ctx, action)
}

// AuthorizeServeApps answers whether this node has granted the actor the right
// to host on it.
func (mod *Module) AuthorizeServeApps(ctx *astral.Context, action *auth.ServeAppsAction) bool {
	return mod.authorizeGrant(ctx, action)
}

// AuthorizeSeeNodeState answers whether this node has granted the actor the
// right to read the node's state.
func (mod *Module) AuthorizeSeeNodeState(ctx *astral.Context, action *auth.SeeNodeStateAction) bool {
	return mod.authorizeGrant(ctx, action)
}

// AuthorizeColdcardScan answers whether this node has granted the actor the
// right to scan the node's attached Coldcard devices.
func (mod *Module) AuthorizeColdcardScan(ctx *astral.Context, action *coldcard.ScanAction) bool {
	return mod.authorizeGrant(ctx, action)
}
