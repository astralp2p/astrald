package user

import (
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// membership returns id's unexpired membership whose link holds, the one that
// expires last when several hold. It returns false when id is expelled.
// visiting holds the identities whose membership is being resolved further up,
// so a cycle of carrier links ends instead of recursing.
func (s *swarmSnapshot) membership(id *astral.Identity, visiting map[string]bool) (*user.Membership, bool) {
	if id.IsZero() || s.expelled(id) || visiting[id.String()] {
		return nil, false
	}
	visiting[id.String()] = true
	defer delete(visiting, id.String())

	var best *user.Membership
	for _, c := range s.contracts {
		m, ok := user.MembershipOf(c)
		if !ok || !m.Subject.IsEqual(id) || m.IssuerSig == nil {
			continue
		}
		if !s.now.Before(m.ExpiresAt.Time()) || !s.counts(m.Issuer) {
			continue
		}
		if !s.linkHolds(m, visiting) {
			continue
		}
		if best == nil || m.ExpiresAt.Time().After(best.ExpiresAt.Time()) {
			best = m
		}
	}
	return best, best != nil
}

// linkHolds reports whether m's issuer may issue it: the root key, at any tier;
// or the Carrier of a JoinRequest that names m's Subject as Joiner at m's tier,
// whose counting Approval m carries out (ApprovalID), that is not declined, and
// whose approver may answer it.
func (s *swarmSnapshot) linkHolds(m *user.Membership, visiting map[string]bool) bool {
	if s.isRoot(m.Issuer) {
		return true
	}
	// why: only the user key issues a Privileged membership (D2).
	if m.Tier == user.TierPrivilegedMember || m.ApprovalID == nil {
		return false
	}
	a, ok := s.approvals[*m.ApprovalID]
	if !ok || a.IsNil() || !s.counts(a.Issuer) {
		return false
	}
	r, _, ok := s.request(a.RequestID)
	if !ok {
		return false
	}
	jr, ok := r.(*user.SignedJoinRequest)
	if !ok || !jr.Joiner.IsEqual(m.Subject) || !jr.Carrier.IsEqual(m.Issuer) || jr.Tier != m.Tier {
		return false
	}
	if s.requestStatus(a.RequestID) == user.RequestDeclined {
		return false
	}
	return s.mayAnswer(a.Issuer, jr, visiting)
}

// mayAnswer reports whether signer may answer req: the root key answers every
// request, any other signer by the tier of its own membership.
//
// why: CheckSwarmEntry already drops a Member's Approval on merge; reading the
// approver's tier here keeps a membership from resting on an Approval that
// reached the set another way, as the chain walk (G3) does.
func (s *swarmSnapshot) mayAnswer(signer *astral.Identity, req astral.Object, visiting map[string]bool) bool {
	if s.isRoot(signer) {
		return true
	}
	m, ok := s.membership(signer, visiting)
	return ok && user.MayAnswer(m.Tier, req)
}

// members returns every identity whose membership holds, in no defined order.
func (v *swarmView) members() []*astral.Identity {
	s := v.snapshot()
	seen := map[string]bool{}
	var ids []*astral.Identity
	for _, c := range s.contracts {
		m, ok := user.MembershipOf(c)
		if !ok || seen[m.Subject.String()] {
			continue
		}
		seen[m.Subject.String()] = true
		if _, ok := s.membership(m.Subject, map[string]bool{}); ok {
			ids = append(ids, m.Subject)
		}
	}
	return ids
}
