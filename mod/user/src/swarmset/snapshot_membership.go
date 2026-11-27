package swarmset

import (
	"slices"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// Membership returns id's membership that holds now (holds): of several, the highest tier, then the one that
// expires last. It returns false when id is expelled.
//
// why the highest tier first: a node that joined again through another carrier holds several memberships, and
// a lower one that expires later must not hide its tier.
func (s *Snapshot) Membership(id *astral.Identity) (*user.Membership, bool) {
	return s.membership(id, map[string]bool{})
}

// membership is Membership with visiting: the identities whose membership is being resolved further up, so a
// cycle of issuers ends instead of recursing.
func (s *Snapshot) membership(id *astral.Identity, visiting map[string]bool) (*user.Membership, bool) {
	if id.IsZero() || s.Expelled(id) || visiting[id.String()] {
		return nil, false
	}
	visiting[id.String()] = true
	defer delete(visiting, id.String())

	var best *user.Membership
	for _, c := range s.contracts {
		m, ok := user.MembershipOf(c)
		if !ok || !m.Subject.IsEqual(id) || !s.holds(m, visiting) {
			continue
		}
		if best == nil || outranks(m, best) {
			best = m
		}
	}
	return best, best != nil
}

// outranks reports whether m is read before other: a higher tier, or the same tier and a later expiry.
func outranks(m, other *user.Membership) bool {
	if a, b := slices.Index(user.Tiers, m.Tier), slices.Index(user.Tiers, other.Tier); a != b {
		return a > b
	}
	return m.ExpiresAt.Time().After(other.ExpiresAt.Time())
}

// holds reports whether the stored membership m counts now: its issuer signed it, it has not expired, its
// issuer is not expelled, it holds its tier's permit alone (user.TierOnly), and its issuer may issue its tier
// (issues).
//
// note: the entry check verified the signatures on merge, so here only their presence is read.
func (s *Snapshot) holds(m *user.Membership, visiting map[string]bool) bool {
	switch {
	case m.IssuerSig == nil:
		return false
	case !s.Now.Before(m.ExpiresAt.Time()) || !s.counts(m.Issuer):
		return false
	case !user.TierOnly(m.SignedContract.Contract, m.Tier):
		return false
	}
	return s.issues(m.Issuer, m.Tier, visiting)
}

// issues reports whether issuer may issue a membership at tier: the root key issues every tier, any other
// issuer by the tier of its own membership that holds now (MayIssue). So a membership holds only while its
// issuer's does.
func (s *Snapshot) issues(issuer *astral.Identity, tier user.Tier, visiting map[string]bool) bool {
	if IsRoot(s.Root, issuer) {
		return true
	}
	m, ok := s.membership(issuer, visiting)
	return ok && MayIssue(m.Tier, tier)
}

// MayIssue is the one rule of who issues which tier, for an issuer that is not the root key: a Privileged member
// issues Member and Trusted, a Trusted member issues Member, and a Member none. Only the root key issues a
// PrivilegedMembership (D2).
// Spec: .ai/system/protocols/user/README.md#memberships
//
// why a table and not the chain: tier permits are not delegable, so no contract chain says who may issue one.
func MayIssue(issuer, tier user.Tier) bool {
	switch issuer {
	case user.TierPrivilegedMember:
		return tier == user.TierMember || tier == user.TierTrusted
	case user.TierTrusted:
		return tier == user.TierMember
	}
	return false
}

// MayAnswer reports whether signer may answer the stored request req: the root key answers every request, any
// other signer by the tier of its own membership (user.MayAnswer). It reports false for an unknown req.
func (s *Snapshot) MayAnswer(signer *astral.Identity, req *astral.ObjectID) bool {
	r, ok := s.Request(req)
	return ok && MayAnswer(s.Root, signer, r.Obj, s.Membership)
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

// ContractCounts reports whether the stored contract sc counts now: both parties signed it, it has not
// expired, neither its issuer nor its subject is expelled, and a membership holds (holds). Its RequestID adds
// nothing. A contract only its issuer signed (an offer) never counts.
func (s *Snapshot) ContractCounts(sc *auth.SignedContract) bool {
	if sc.IsNil() || sc.IssuerSig == nil || sc.SubjectSig == nil {
		return false
	}
	if !s.Now.Before(sc.ExpiresAt.Time()) || !s.counts(sc.Issuer) || s.Expelled(sc.Subject) {
		return false
	}
	if m, ok := user.MembershipOf(sc); ok {
		return s.holds(m, map[string]bool{})
	}
	return true
}
