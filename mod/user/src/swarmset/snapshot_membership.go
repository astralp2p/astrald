package swarmset

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// Membership returns id's unexpired membership whose link holds, the one that expires last when several hold. It
// returns false when id is expelled.
func (s *Snapshot) Membership(id *astral.Identity) (*user.Membership, bool) {
	return s.membership(id, map[string]bool{})
}

// membership is Membership with visiting: the identities whose membership is being resolved further up, so a
// cycle of carrier links ends instead of recursing.
func (s *Snapshot) membership(id *astral.Identity, visiting map[string]bool) (*user.Membership, bool) {
	if id.IsZero() || s.Expelled(id) || visiting[id.String()] {
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
		if !s.Now.Before(m.ExpiresAt.Time()) || !s.counts(m.Issuer) {
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

// linkHolds reports whether m's issuer may issue it: the root key, at any tier; or the Carrier of a JoinRequest
// that names m's Subject as Joiner at m's tier, when m holds that tier's permit alone, whose counting Approval m
// carries out (ApprovalID), that is not declined, and whose approver may answer it.
func (s *Snapshot) linkHolds(m *user.Membership, visiting map[string]bool) bool {
	if IsRoot(s.Root, m.Issuer) {
		return true
	}
	// why: only the user key issues a Privileged membership (D2).
	if m.Tier == user.TierPrivilegedMember || m.ApprovalID == nil {
		return false
	}
	a, ok := s.approvals[*m.ApprovalID]
	if !ok || !s.counts(a.Issuer) {
		return false
	}
	r, ok := s.Request(a.RequestID)
	if !ok {
		return false
	}
	jr, ok := r.Obj.(*user.SignedJoinRequest)
	if !ok || !jr.Joiner.IsEqual(m.Subject) || !jr.Carrier.IsEqual(m.Issuer) || jr.Tier != m.Tier {
		return false
	}
	// why: user.ContractTier ignores permits that name no tier, so a carrier could sign a membership with more
	// than the tier the join asked for.
	if !user.TierOnly(m.SignedContract.Contract, m.Tier) {
		return false
	}
	if s.RequestStatus(a.RequestID) == user.RequestDeclined {
		return false
	}
	return s.mayAnswer(a.Issuer, jr, visiting)
}

// MayAnswer reports whether signer may answer the stored request req: the root key answers every request, any
// other signer by the tier of its own membership (user.MayAnswer). It reports false for an unknown req.
func (s *Snapshot) MayAnswer(signer *astral.Identity, req *astral.ObjectID) bool {
	r, ok := s.Request(req)
	return ok && s.mayAnswer(signer, r.Obj, map[string]bool{})
}

// mayAnswer is MayAnswer for the request object req, with visiting as in membership.
//
// why: the entry check already drops a Member's Approval on merge; reading the approver's tier here keeps a
// membership from resting on an Approval that reached the set another way, as the chain walk does.
func (s *Snapshot) mayAnswer(signer *astral.Identity, req astral.Object, visiting map[string]bool) bool {
	return MayAnswer(s.Root, signer, req, func(id *astral.Identity) (*user.Membership, bool) {
		return s.membership(id, visiting)
	})
}

// MayAnswer is the one answer rule: the root key root answers every request req, any other signer by the tier
// of the membership that membership returns for it (user.MayAnswer).
func MayAnswer(root, signer *astral.Identity, req astral.Object, membership func(*astral.Identity) (*user.Membership, bool)) bool {
	if IsRoot(root, signer) {
		return true
	}
	m, ok := membership(signer)
	return ok && user.MayAnswer(m.Tier, req)
}

// Members returns every identity whose membership holds, in no defined order.
func (s *Snapshot) Members() []*astral.Identity {
	seen := map[string]bool{}
	var ids []*astral.Identity
	for _, c := range s.contracts {
		m, ok := user.MembershipOf(c)
		if !ok || seen[m.Subject.String()] {
			continue
		}
		seen[m.Subject.String()] = true
		if _, ok := s.Membership(m.Subject); ok {
			ids = append(ids, m.Subject)
		}
	}
	return ids
}

// Contracts returns every stored contract by entry ID.
func (s *Snapshot) Contracts() map[astral.ObjectID]*auth.SignedContract {
	return s.contracts
}

// ContractCounts reports whether the stored contract sc counts now: it has not expired, neither its issuer nor
// its subject is expelled, and
//   - a membership: its link holds (linkHolds);
//   - a contract carrying out an Approval (ApprovalID): that Approval counts and its request is approved;
//   - any other contract: nothing more.
func (s *Snapshot) ContractCounts(sc *auth.SignedContract) bool {
	if sc.IsNil() || !s.Now.Before(sc.ExpiresAt.Time()) || !s.counts(sc.Issuer) || s.Expelled(sc.Subject) {
		return false
	}
	if m, ok := user.MembershipOf(sc); ok {
		return m.IssuerSig != nil && s.linkHolds(m, map[string]bool{})
	}
	if sc.ApprovalID == nil {
		return true
	}
	a, ok := s.approvals[*sc.ApprovalID]
	return ok && s.counts(a.Issuer) && s.RequestStatus(a.RequestID) == user.RequestApproved
}
