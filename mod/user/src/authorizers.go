package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeSeeSwarm allows the swarm's user and current swarm members to read
// the swarm's state.
//
// why: the policy is AuthorizeInfo's, carried over unchanged. SeeSwarm covers
// five reads beside user.info, and those five were unauthorized entirely — so no
// caller that could read before loses access here, and none gains any.
//
// why the whole swarm and not the user alone: a sibling syncing assets routes
// user.sync_assets as the user (sync.go), and a local caller carrying no
// identity is promoted to this node's identity (core/router.go), which is a
// member. Narrowing to the issuer would break both.
//
// why members of every tier: Members may see requests (vision, 2026-11-24).
func (mod *Module) AuthorizeSeeSwarm(ctx *astral.Context, a *user.SeeSwarmAction) bool {
	userID := mod.Identity()
	if userID.IsZero() || a.Actor().IsZero() {
		return false
	}
	if a.Actor().IsEqual(userID) {
		return true
	}
	_, ok := mod.Membership(a.Actor())
	return ok
}

// AuthorizeAdminSwarm allows the swarm's user to change what the swarm holds,
// and nobody else.
//
// why: the active contract's issuer alone. user.add_asset and
// user.remove_asset had no authorization at all and tighten to it.
//
// why narrower than SeeSwarm, which grants the whole local swarm: granting the
// swarm here would let any member change what every other one holds.
//
// why this node's own identity is not in the grant, unlike AuthorizeAdminObjects:
// a local caller carrying no identity is promoted to it, so including it would
// let any unauthenticated local caller change the swarm. Managing the swarm from
// the command line requires the user's identity, which is what it required
// before this change.
func (mod *Module) AuthorizeAdminSwarm(ctx *astral.Context, a *user.AdminSwarmAction) bool {
	userID := mod.Identity()
	return !userID.IsZero() && a.Actor().IsEqual(userID)
}

// AuthorizeExpel allows the user, and nobody else, to expel (D7). The swarm
// root handler never answers an expel, so a Privileged node cannot.
func (mod *Module) AuthorizeExpel(ctx *astral.Context, a *user.ExpelAction) bool {
	userID := mod.Identity()
	return !userID.IsZero() && a.Actor().IsEqual(userID)
}

// AuthorizeStoreObjects grants object writes to the user identity and to this
// node itself, and to nobody else.
//
// why: the ops StoreObjects covers had no authorization at all, so there is no
// policy to carry over. An app holding no permits is refused, which is the point.
//
// why the swarm is not in the grant: the swarm held it for objects.push, which
// asks no action (mod/objects/src/op_push.go). A sibling writes through a
// contract chain.
//
// why this node's own identity is granted: a local caller carrying no identity is
// promoted to it (core/router.go), which is how the CLI and apphost reach these
// ops — including the user-provisioning ceremony, which stores the derived user
// key on an unclaimed node before any user or swarm exists.
func (mod *Module) AuthorizeStoreObjects(ctx *astral.Context, a *auth.StoreObjectsAction) bool {
	return mod.authorizeUserOrNode(a.Actor())
}

// AuthorizeAdminObjects grants destructive object calls to the user identity and to
// this node itself, and to nobody else.
//
// why: a sibling attaching a repository to a host directory is the defect this
// action was added to close, so granting the swarm here would leave that path
// open to every member. This node's own identity is in the grant because a session with the node's token acts as it, which is how
// the CLI reaches these ops; a token-less local caller is promoted to it too
// (core/router.go), within apphost's gate.
//
// Widening this to siblings is a decision the node's operator makes, not a default.
func (mod *Module) AuthorizeAdminObjects(ctx *astral.Context, a *auth.AdminObjectsAction) bool {
	return mod.authorizeUserOrNode(a.Actor())
}
