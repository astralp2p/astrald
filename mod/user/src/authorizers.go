package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeSeeSwarm allows the user (userOrSelf) and current swarm members to
// read the swarm's state.
//
// why: the policy is AuthorizeInfo's, carried over unchanged. SeeSwarm covers
// five reads beside user.info, and those five were unauthorized entirely — so no
// caller that could read before loses access here, and none gains any.
//
// why the whole swarm and not the user alone: a sibling's alias pull queries
// user.info as that node (sync.go), and a local caller carrying no identity is
// promoted to this node's identity (core/router.go), which is a member.
// Narrowing to the issuer would break both.
//
// why members of every tier: Members may see requests (vision, 2026-11-24).
func (mod *Module) AuthorizeSeeSwarm(ctx *astral.Context, a *user.SeeSwarmAction) bool {
	if a.Actor().IsZero() {
		return false
	}
	if a.Actor().IsEqual(mod.userOrSelf()) {
		return true
	}
	_, ok := mod.Membership(a.Actor())
	return ok
}

// AuthorizeAdminSwarm allows the user (userOrSelf) to change what the swarm
// holds. A contract the user signs delegates it through the auth walk.
//
// note: a Privileged node administers the swarm itself through
// AuthorizeAdminSwarmAsPrivileged, which passes nothing on.
//
// why narrower than SeeSwarm, which grants the whole local swarm: membership is
// granted on request today, so granting the swarm here would let a node it
// admitted expel every other one.
//
// why a claimed node's own identity is not in the grant, unlike
// AuthorizeAdminObjects: a local caller carrying no identity is promoted to it,
// so including it would let any unauthenticated local caller adopt and expel.
// Managing the swarm from the command line requires the user's identity.
// note: an unclaimed node is the user (userOrSelf), so there the promoted local
// caller holds the action.
func (mod *Module) AuthorizeAdminSwarm(ctx *astral.Context, a *user.AdminSwarmAction) bool {
	return !a.Actor().IsZero() && a.Actor().IsEqual(mod.userOrSelf())
}

// AuthorizeExpel allows the user (userOrSelf) to expel (D7). A contract the
// user signs delegates it through the auth walk; no membership grants it.
func (mod *Module) AuthorizeExpel(ctx *astral.Context, a *user.ExpelAction) bool {
	return !a.Actor().IsZero() && a.Actor().IsEqual(mod.userOrSelf())
}

// AuthorizeStoreObjects grants object writes to the user identity and to this
// node itself, and to nobody else.
//
// why: the ops StoreObjects covers had no authorization at all, so there is no
// policy to carry over. An app holding no permits is refused, which is the point.
//
// why the swarm is not in the grant: the swarm held it for objects.push, which
// asks no action (mod/objects/src/op_push.go). A sibling writes through a
// node-local grant or a signed contract.
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
// action was added to close, and a swarm membership is granted on request today, so
// granting the swarm here would leave that path open. This node's own identity is in
// the grant because a session with the node's token acts as it, which is how
// the CLI reaches these ops; a token-less local caller is promoted to it too
// (core/router.go), within apphost's gate.
//
// Widening this to siblings is a decision the node's operator makes, not a default.
// Replacing the handler with a root rule and contract-issued grants is stage 2.
func (mod *Module) AuthorizeAdminObjects(ctx *astral.Context, a *auth.AdminObjectsAction) bool {
	return mod.authorizeUserOrNode(a.Actor())
}
