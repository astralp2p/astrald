package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// chainRules holds the rules that read swarm memberships: a swarm member is
// not enough, and what a member or its app may do beyond them comes from its
// contract chain (the auth walk).
//
// note: AuthorizeStoreObjects leaves the swarm out, so it has no narrowed rule.
type chainRules struct {
	mod     *Module
	members membershipReader
}

func newChainRules(mod *Module, members membershipReader) *chainRules {
	return &chainRules{mod: mod, members: members}
}

// AuthorizeRelayFor allows a node holding a PrivilegedMembership the user
// signed to relay queries on behalf of the user itself. deps.go registers it
// NodeLocal.
//
// why: a Member or Trusted node does not relay as the user (D5, D6).
// note: the user relays as itself through the nodes module's rule, which
// contracts the user signs delegate.
// why Identity and not userOrSelf: the rule answers for a PrivilegedMembership
// a claimed user signed. An unclaimed node relays as itself through the nodes
// module's rule.
func (r *chainRules) AuthorizeRelayFor(ctx *astral.Context, a *nodes.RelayForAction) bool {
	userID := r.mod.Identity()
	if a.Actor().IsZero() || userID.IsZero() || !a.ForID.IsEqual(userID) {
		return false
	}
	m, ok := r.members.Membership(a.Actor())
	return ok && usermod.PrivilegedBy(m, userID)
}

// AuthorizeAdminSwarmAsPrivileged allows a node holding a PrivilegedMembership
// the user signed to administer the swarm itself. deps.go registers it
// NodeLocal.
//
// why NodeLocal: a Privileged node answers swarm requests for the user, but no
// contract it signs passes that on (D5).
// note: the user's own rule, which contracts the user signs delegate, is
// AuthorizeAdminSwarm.
// why Identity and not userOrSelf: the rule answers for a PrivilegedMembership
// a claimed user signed. An unclaimed node administers its swarm through
// AuthorizeAdminSwarm.
func (r *chainRules) AuthorizeAdminSwarmAsPrivileged(ctx *astral.Context, a *user.AdminSwarmAction) bool {
	userID := r.mod.Identity()
	if a.Actor().IsZero() || userID.IsZero() {
		return false
	}
	m, ok := r.members.Membership(a.Actor())
	return ok && usermod.PrivilegedBy(m, userID)
}

// AuthorizeServiceDiscovery allows the user (userOrSelf) to discover every
// service and refuses every other identity; a member or its app discovers
// through its contract chain.
//
// why a claimed node's own identity is not in the rule: as in
// AuthorizeServeObjects, a local caller carrying no identity is promoted to it
// (core/router.go). An unclaimed node is the user (userOrSelf).
func (r *chainRules) AuthorizeServiceDiscovery(ctx *astral.Context, a *services.ServiceDiscoveryAction) bool {
	actor := a.Actor()
	return !actor.IsZero() && actor.IsEqual(r.mod.userOrSelf())
}

// AuthorizeSeeObjects grants object reads to the user identity and to this
// node itself; a member or its app reads through its contract chain.
//
// why this node's own identity stays: the local caller with no identity is
// parked by the operator (2026-11-25), so its promotion keeps today's reads.
func (r *chainRules) AuthorizeSeeObjects(ctx *astral.Context, a *auth.SeeObjectsAction) bool {
	return r.mod.authorizeUserOrNode(a.Actor())
}
