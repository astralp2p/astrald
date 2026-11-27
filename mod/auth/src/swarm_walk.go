package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// swarmWalk is what Authorize reads from the swarm view (swarmReader).
type swarmWalk struct {
	view swarmReader
	root func() *astral.Identity
}

// swarm returns what the walk reads from SwarmState, and nil without the user
// module: Authorize then behaves as without a swarm.
func (mod *Module) swarm() *swarmWalk {
	if mod.User == nil {
		return nil
	}
	return &swarmWalk{view: mod.User, root: mod.rootKey}
}

// rootKey returns the root key: the user this node is claimed by, or this node
// while it is unclaimed.
func (mod *Module) rootKey() *astral.Identity {
	if id := mod.User.Identity(); !id.IsZero() {
		return id
	}
	return mod.node.Identity()
}

// swarmRoot reports whether the action's actor holds the user's authority:
// it is the root key, or holds a PrivilegedMembership the root key signed, and
// is not expelled. It answers at any hop, for every action but an expel and a
// sudo.
//
// why AdminSwarmAction is answered: user.expel asks ExpelAction since G7, and
// the ops that ask AdminSwarmAction sign nothing with the user key; answering
// requests is what a Privileged node, or an unclaimed node, does for the user.
//
// The root answers no SudoAction: acting as another identity is signing and
// querying with its key, and as the user key a Privileged node would confirm
// a Privileged join (D2), expel (D7) or sign with a software user key. Sudo
// stays what AuthorizeSudo and explicit contracts allow, as before G7.
//
// A Privileged node relays only as the user (D6), not as another node or
// another node's app.
func (mod *Module) swarmRoot(action auth.ActionObject) bool {
	s := mod.swarm()
	if s == nil {
		return false
	}
	switch action.(type) {
	case *user.ExpelAction, *auth.SudoAction:
		return false
	}

	actor := action.Actor()
	if actor.IsZero() || s.view.Expelled(actor) {
		return false
	}

	root := s.root()
	if root.IsZero() {
		return false
	}
	if actor.IsEqual(root) {
		return true
	}

	if a, ok := action.(*nodes.RelayForAction); ok && !a.ForID.IsEqual(root) {
		return false
	}

	m, ok := s.view.Membership(actor)
	return ok && usermod.PrivilegedBy(m, root)
}

// userPowerFromNonRoot reports whether sc is a contract whose issuer is not
// the root key and action is one of the user's four powers: an expel,
// AdminSwarm, a relay as the user, or a sudo as the user. The walk does not
// follow such a contract for such an action: only a contract the root key
// issued grants one of them.
// Spec: .ai/system/protocols/auth/README.md#authority-in-a-swarm
//
// why every non-root contract: swarmRoot answers for a Privileged member at
// any hop, so a contract it issues would otherwise pass AdminSwarm or a relay
// as the user on to its subject (D5, D6). The member still uses them itself,
// as the actor.
//
// why an app's own relay contract stays: it names the app itself as ForID,
// its issuer's own identity, not the user.
func (mod *Module) userPowerFromNonRoot(sc *auth.SignedContract, action auth.ActionObject) bool {
	s := mod.swarm()
	if s == nil {
		return false
	}
	root := s.root()
	if root.IsZero() || sc.Issuer.IsEqual(root) {
		return false
	}
	switch a := action.(type) {
	case *user.ExpelAction, *user.AdminSwarmAction:
		return true
	case *nodes.RelayForAction:
		return a.ForID.IsEqual(root)
	case *auth.SudoAction:
		return a.AsID.IsEqual(root)
	}
	return false
}
