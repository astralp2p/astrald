package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	usermod "github.com/astralp2p/astrald/mod/user"
)

var _ authmod.SwarmWalk = &Module{}

// swarmWalk is what Authorize reads from SwarmState once SetSwarm has run.
type swarmWalk struct {
	view  usermod.SwarmView
	state nodesmod.SwarmState
	root  func() *astral.Identity
}

// SetSwarm makes Authorize read SwarmState (see authmod.SwarmWalk). A nil
// view, state or root clears it, and Authorize behaves as without it.
func (mod *Module) SetSwarm(view usermod.SwarmView, state nodesmod.SwarmState, root func() *astral.Identity) {
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
// ApprovalID, it is the issuer, as before.
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
// its issuer carried in for its subject at the tier sc grants.
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
		tier, ok := user.ContractTier(sc.Contract)
		return ok && tier == r.Tier
	}
	return false
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
