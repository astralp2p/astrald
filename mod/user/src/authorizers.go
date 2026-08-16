package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeExpel allows the swarm's user to expel nodes.
func (mod *Module) AuthorizeExpel(ctx *astral.Context, a *user.ExpelAction) bool {
	ac := mod.ActiveContract()
	return ac != nil && a.Actor().IsEqual(ac.Issuer)
}

// AuthorizeAdopt allows the swarm's user to adopt nodes.
func (mod *Module) AuthorizeAdopt(ctx *astral.Context, a *user.AdoptAction) bool {
	ac := mod.ActiveContract()
	return ac != nil && a.Actor().IsEqual(ac.Issuer)
}

// AuthorizeInfo allows the swarm's user and current swarm members to read contract info.
func (mod *Module) AuthorizeInfo(ctx *astral.Context, a *user.InfoAction) bool {
	ac := mod.ActiveContract()
	if ac == nil {
		return false
	}
	if a.Actor().IsEqual(ac.Issuer) {
		return true
	}
	for _, nodeID := range mod.LocalSwarm() {
		if nodeID.IsEqual(a.Actor()) {
			return true
		}
	}
	return false
}

// AuthorizeRelayFor allows a swarm node to relay queries on behalf of the local user.
func (mod *Module) AuthorizeRelayFor(ctx *astral.Context, a *nodes.RelayForAction) bool {
	if !a.ForID.IsEqual(mod.Identity()) {
		return false
	}
	for _, nodeID := range mod.LocalSwarm() {
		if nodeID.IsEqual(a.Actor()) {
			return true
		}
	}
	return false
}

// AuthorizeSeeObjects grants object reads to the user identity itself and to any node in the local swarm.
//
// why: the policy is carried over unchanged from the retired per-object read handler.
// SeeObjects covers every read op in mod/objects, not just objects.read, but the eleven ops it
// adds were unauthorized entirely — so no caller that could read before loses access here, and
// none gains any. Replacing the policy is stage 2 of the parent task, not this change.
func (mod *Module) AuthorizeSeeObjects(ctx *astral.Context, a *auth.SeeObjectsAction) bool {
	if a.Actor().IsEqual(mod.Identity()) {
		return true
	}

	for _, nodeID := range mod.LocalSwarm() {
		if nodeID.IsEqual(a.Actor()) {
			return true
		}
	}

	return false
}

// AuthorizeStoreObjects grants object writes to the user identity itself and to any node in
// the local swarm.
//
// why: the eight ops StoreObjects covers had no authorization at all, so there is no policy
// to carry over. This is the narrowest rule that keeps the node working: the swarm pushes
// signed contracts and sibling notifications at objects.push, and a local caller with no
// caller identity is the node itself (core/router.go). An app holding no permits is refused,
// which is the point. Replacing this handler with a root rule and contract-issued grants is
// stage 2 of the parent task.
func (mod *Module) AuthorizeStoreObjects(ctx *astral.Context, a *auth.StoreObjectsAction) bool {
	if a.Actor().IsEqual(mod.Identity()) {
		return true
	}

	for _, nodeID := range mod.LocalSwarm() {
		if nodeID.IsEqual(a.Actor()) {
			return true
		}
	}

	return false
}

// AuthorizeAdminObjects grants destructive object calls to the user identity and to
// this node itself, and to nobody else.
//
// why: narrower than the SeeObjects and StoreObjects handlers, which grant the whole
// local swarm. A sibling attaching a repository to a host directory is the defect this
// action was added to close, and a swarm membership is granted on request today, so
// granting the swarm here would leave that path open. This node's own identity is in
// the grant because a local caller carrying no identity is promoted to it
// (core/router.go), which is how the CLI and apphost reach these ops.
//
// Widening this to siblings is a decision the node's operator makes, not a default.
// Replacing the handler with a root rule and contract-issued grants is stage 2.
func (mod *Module) AuthorizeAdminObjects(ctx *astral.Context, a *auth.AdminObjectsAction) bool {
	if a.Actor().IsEqual(mod.Identity()) {
		return true
	}

	return a.Actor().IsEqual(mod.node.Identity())
}
