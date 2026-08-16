package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/objects"
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

// AuthorizeReadObject grants read access to the user identity itself and to any node in the local swarm.
func (mod *Module) AuthorizeReadObject(ctx *astral.Context, a *objects.ReadObjectAction) bool {
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
