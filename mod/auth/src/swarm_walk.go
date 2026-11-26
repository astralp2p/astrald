package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
	usermod "github.com/astralp2p/astrald/mod/user"
)

var _ authmod.SwarmWalk = &Module{}

// swarmWalk is what Authorize reads from SwarmState once SetSwarm has run.
type swarmWalk struct {
	view  usermod.SwarmView
	state usermod.SwarmState
	root  func() *astral.Identity
}

// SetSwarm makes Authorize read SwarmState (see authmod.SwarmWalk). A nil
// view, state or root clears it, and Authorize behaves as without it.
func (mod *Module) SetSwarm(view usermod.SwarmView, state usermod.SwarmState, root func() *astral.Identity) {
	if view == nil || state == nil || root == nil {
		mod.swarm.Store(nil)
		return
	}
	mod.swarm.Store(&swarmWalk{view: view, state: state, root: root})
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
	s := mod.swarm.Load()
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
	return ok && m.Tier == user.TierPrivilegedMember && m.Issuer.IsEqual(root)
}

// contractLink returns the identity the walk continues as above sc, and false
// when the link does not hold. Without a swarm, and for a contract with no
// ApprovalID, it is the issuer, as before; such a contract carries no user
// authority from an issuer that is not the root (unlinkedUserAuthority).
func (mod *Module) contractLink(sc *auth.SignedContract) (*astral.Identity, bool) {
	s := mod.swarm.Load()
	if s == nil || sc.ApprovalID == nil {
		return sc.Issuer, true
	}

	approver, ok := s.approvalLink(sc)
	if !ok {
		return nil, false
	}

	// why: the walk skips the issuer's level, so its veto is asked here.
	if mod.vetoed(sc.Issuer) {
		return nil, false
	}
	return approver, true
}

// unlinkedUserAuthority reports whether sc is a contract with no ApprovalID,
// from an issuer that is not the root key, and action is the user's authority:
// a relay as the user, AdminSwarm, a sudo as the user, or an expel. The walk
// does not follow such a contract for such an action.
//
// why: an unlinked contract continues the walk at its issuer, so a Privileged
// node, which the root answers for, could otherwise sign a Member a contract
// to relay as the user or administer the swarm (D5, D6), with no Approval and
// so with no record any other node checks.
//
// why unlinked contracts stay for every other action: an app's relay contract
// (apphost.NewAppContract) names the app itself as ForID, its issuer's own
// identity, and an app delegating its own permits names no user authority.
// The root key's own contracts stay: the user signed them.
func (mod *Module) unlinkedUserAuthority(sc *auth.SignedContract, action auth.ActionObject) bool {
	s := mod.swarm.Load()
	if s == nil || sc.ApprovalID != nil {
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

// approvalLink checks the Approval sc carries out and returns its signer.
func (s *swarmWalk) approvalLink(sc *auth.SignedContract) (*astral.Identity, bool) {
	obj, err := s.state.SwarmEntry(sc.ApprovalID)
	if err != nil {
		return nil, false
	}
	approval, ok := obj.(*user.SignedApproval)
	if !ok || approval.IsNil() || approval.RequestID == nil || approval.Issuer.IsZero() {
		return nil, false
	}

	// why the status and not only the Approval: a Decline beats it, as does an
	// expel of the request's filer, carrier or subject.
	if s.view.RequestStatus(approval.RequestID) != user.RequestApproved {
		return nil, false
	}

	req, err := s.state.SwarmEntry(approval.RequestID)
	if err != nil || !linksTo(req, sc) {
		return nil, false
	}

	for _, id := range []*astral.Identity{sc.Issuer, sc.Subject, approval.Issuer} {
		if s.view.Expelled(id) {
			return nil, false
		}
	}

	if !s.mayApprove(approval.Issuer, req) {
		return nil, false
	}
	return approval.Issuer, true
}

// linksTo reports whether req is a request sc may carry out: a ContractRequest
// its issuer filed for its subject that covers its permits, or a JoinRequest
// its issuer carried in for its subject when sc grants that tier's permit
// alone.
//
// why the subject is checked: an Approval answers one identity's request, so a
// host cannot reuse it for another app.
func linksTo(req astral.Object, sc *auth.SignedContract) bool {
	switch r := req.(type) {
	case *user.SignedContractRequest:
		return !r.IsNil() &&
			r.FiledBy.IsEqual(sc.Issuer) &&
			r.Identity.IsEqual(sc.Subject) &&
			r.Covers(sc.Contract)
	case *user.SignedJoinRequest:
		if r.IsNil() || !r.Carrier.IsEqual(sc.Issuer) || !r.Joiner.IsEqual(sc.Subject) {
			return false
		}
		return tierOnly(sc.Contract, r.Tier)
	}
	return false
}

// tierOnly reports whether c holds tier's permit and nothing else: one
// non-delegable, unconstrained permit, as user.NewMembership builds it.
//
// why: user.ContractTier ignores permits that name no tier, so a carrier could
// add RelayFor or AdminSwarm to a membership it signs and the walk would take
// them up to the approver, whose authority the root answers (D5, D6).
func tierOnly(c *auth.Contract, tier user.Tier) bool {
	if c == nil || len(c.Permits) != 1 || c.Permits[0] == nil {
		return false
	}
	p := c.Permits[0]
	return string(p.Action) == tier.Action() && p.Delegation == 0 &&
		(p.Constraints == nil || len(p.Constraints.Objects()) == 0)
}

// mayApprove reports whether approver's Approval counts for req: the root key's
// always does, any other approver's only when its tier may answer req.
func (s *swarmWalk) mayApprove(approver *astral.Identity, req astral.Object) bool {
	if root := s.root(); !root.IsZero() && approver.IsEqual(root) {
		return true
	}
	m, ok := s.view.Membership(approver)
	return ok && user.MayAnswer(m.Tier, req)
}
