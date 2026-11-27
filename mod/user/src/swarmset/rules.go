package swarmset

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// Verifier is the part of the crypto module the entry check uses.
type Verifier interface {
	Verify(key *crypto.PublicKey, sig *crypto.Signature, obj crypto.SignableTextObject) error
}

// Rules is the entry check (usermod.SwarmState): which tier may write which entry (D2, D5, D7, D10). Each entry
// kind has a gating identity whose tier, read from its stored membership, decides:
//   - ContractRequest: FiledBy, any tier;
//   - JoinRequest: the Carrier, Trusted or Privileged; the Joiner only signs it, and its Tier must be valid;
//   - ExpelRequest: FiledBy, Trusted or Privileged;
//   - Approval: the signer, whose tier must answer the ExpelRequest named by RequestID (user.MayAnswer); only an
//     ExpelRequest has an Approval;
//   - Decline: the signer, whose tier must answer the request named by RequestID (user.MayAnswer);
//   - contract signed by both parties (contractGate): a membership, gated on the Issuer's tier issuing its tier
//     (MayIssue); a contract the root key issued; or a contract that names a stored ContractRequest, as the
//     approver's contract to its filer or the filer's contract to the identity it names;
//   - offer, a contract only its Issuer signed (offerGate): the Issuer, whose tier must answer the
//     ContractRequest it names;
//   - Expulsion: only the root key (D7).
//
// The root key needs no membership and may write every kind. Check returns:
//   - user.EntryDrop: obj is not an entry, a signature does not verify, a request's Timeout is not valid, the
//     gating identity's tier may not write it, or a contract or offer fails its request's rules;
//   - user.EntryWait: the gating identity's membership, or the request an answer, contract or offer names, is
//     not stored yet; an offer waits only for its request;
//   - user.EntryAccept: otherwise.
//
// An entry that was valid when it arrived stays valid; expels apply when entries are read, except that an
// offer from an expelled issuer, or for a declined request, is dropped.
// Spec: .ai/system/protocols/user/README.md#two-signatures
type Rules struct {
	verifier Verifier
	root     func() *astral.Identity
}

var _ Checker = &Rules{}

// NewRules returns the entry check. root returns the root key: the user the node is claimed by, or the node
// itself while it is unclaimed.
func NewRules(verifier Verifier, root func() *astral.Identity) *Rules {
	return &Rules{verifier: verifier, root: root}
}

// Check decides whether obj may enter the set. Signatures and Timeouts are checked first, then the gating
// identity's tier (tierGate); the root key writes every kind.
//
// note: Membership excludes an expelled identity, so an entry gated by one waits instead of entering.
//
// why the root is read once: a claim can change it between two reads, and the gate must be judged against the
// root it was built for.
func (c *Rules) Check(obj astral.Object, r Reader) user.EntryVerdict {
	root := c.root()

	switch e := obj.(type) {
	case *user.SignedContractRequest:
		if e.IsNil() || !e.ValidTimeout() || !c.verify(e.FiledBy, e.FiledBySig, e.ContractRequest) {
			return user.EntryDrop
		}
		return tierGate(r, root, e.FiledBy, user.Tier.IsValid)

	case *user.SignedJoinRequest:
		if e.IsNil() || !e.ValidTimeout() || !e.Tier.IsValid() || !c.verify(e.Joiner, e.JoinerSig, e.JoinRequest) {
			return user.EntryDrop
		}
		return tierGate(r, root, e.Carrier, trustedOrAbove)

	case *user.SignedExpelRequest:
		if e.IsNil() || !e.ValidTimeout() || !c.verify(e.FiledBy, e.FiledBySig, e.ExpelRequest) {
			return user.EntryDrop
		}
		return tierGate(r, root, e.FiledBy, trustedOrAbove)

	case *user.SignedApproval:
		if e.IsNil() || !c.verify(e.Issuer, e.IssuerSig, e.Approval) {
			return user.EntryDrop
		}
		// why: a contract request is answered by an offer and a join by its carrier's signature, so only an
		// ExpelRequest has an Approval.
		req, verdict := requestOf(e.RequestID, r)
		switch {
		case verdict != user.EntryAccept:
			return verdict
		case !req.isExpel():
			return user.EntryDrop
		}
		return tierGate(r, root, e.Issuer, func(t user.Tier) bool { return user.MayAnswer(t, req.Obj) })

	case *user.SignedDecline:
		if e.IsNil() || !c.verify(e.Issuer, e.IssuerSig, e.Decline) {
			return user.EntryDrop
		}
		req, verdict := requestOf(e.RequestID, r)
		if verdict != user.EntryAccept {
			return verdict
		}
		return tierGate(r, root, e.Issuer, func(t user.Tier) bool { return user.MayAnswer(t, req.Obj) })

	case *auth.SignedContract:
		return c.contractGate(e, root, r)

	case *user.SignedExpulsion:
		// why: only the user expels (D7).
		if e.Expulsion == nil || !IsRoot(root, e.Issuer) || !c.verify(e.Issuer, e.IssuerSig, e.Expulsion) {
			return user.EntryDrop
		}
		return user.EntryAccept
	}
	return user.EntryDrop
}

// tierGate decides an entry on the tier of id, its gating identity: the root key is accepted; an identity with no
// membership waits; one whose tier allows does not accept.
func tierGate(r Reader, root, id *astral.Identity, allows func(user.Tier) bool) user.EntryVerdict {
	if IsRoot(root, id) {
		return user.EntryAccept
	}
	m, ok := r.Snapshot(root).Membership(id)
	if !ok {
		return user.EntryWait
	}
	if !allows(m.Tier) {
		return user.EntryDrop
	}
	return user.EntryAccept
}

func trustedOrAbove(t user.Tier) bool {
	return t == user.TierTrusted || t == user.TierPrivilegedMember
}

// requestOf returns the stored request an Approval or Decline names as its RequestID. It waits while the request
// is not stored, and drops an answer that names no request or another entry.
func requestOf(id *astral.ObjectID, r Reader) (Request, user.EntryVerdict) {
	if id == nil {
		return Request{}, user.EntryDrop
	}
	obj, ok := r.Entry(id)
	if !ok {
		return Request{}, user.EntryWait
	}
	req, ok := ParseRequest(obj)
	if !ok {
		return Request{}, user.EntryDrop
	}
	return req, user.EntryAccept
}

// contractGate gates a contract. Its IssuerSig is required; a contract without a SubjectSig is an offer
// (offerGate). With both signatures it is stored only as:
//   - a membership, gated on its issuer's tier issuing its tier (MayIssue); a PrivilegedMembership is issued
//     only by the root key (D2);
//   - a contract the root key issued;
//   - a contract that names a stored ContractRequest as its RequestID: the approver's contract to the filer,
//     whose permits follow the offer depth rule (user.ContractRequest.CoversOffer) and whose issuer may answer
//     the request; or the filer's contract to the identity the request names, whose permits are requested ones
//     (user.ContractRequest.Covers).
//
// why so narrow: SwarmState only grows and every member syncs it, so a contract enters only for a reason the
// swarm can check.
func (c *Rules) contractGate(sc *auth.SignedContract, root *astral.Identity, r Reader) user.EntryVerdict {
	if sc.IsNil() || !c.verify(sc.Issuer, sc.IssuerSig, sc.Contract) {
		return user.EntryDrop
	}
	if sc.SubjectSig == nil {
		return offerGate(sc, root, r)
	}
	if !c.verify(sc.Subject, sc.SubjectSig, sc.Contract) {
		return user.EntryDrop
	}
	if tier, ok := user.ContractTier(sc.Contract); ok {
		if tier == user.TierPrivilegedMember && !IsRoot(root, sc.Issuer) {
			return user.EntryDrop
		}
		return tierGate(r, root, sc.Issuer, func(issuer user.Tier) bool { return MayIssue(issuer, tier) })
	}
	if IsRoot(root, sc.Issuer) {
		return user.EntryAccept
	}
	req, verdict := contractRequestOf(sc, r)
	switch {
	case verdict != user.EntryAccept:
		return verdict
	case sc.Subject.IsEqual(req.FiledBy) && req.CoversOffer(sc.Contract):
		return tierGate(r, root, sc.Issuer, func(t user.Tier) bool { return user.MayAnswer(t, req) })
	case sc.Issuer.IsEqual(req.FiledBy) && sc.Subject.IsEqual(req.Identity) && req.Covers(sc.Contract):
		return tierGate(r, root, sc.Issuer, user.Tier.IsValid)
	}
	return user.EntryDrop
}

// offerGate gates an offer, a contract only its issuer signed. It is stored only when its RequestID names a
// stored ContractRequest whose FiledBy is its subject, its permits follow the offer depth rule
// (user.ContractRequest.CoversOffer), its issuer may answer the request and is not expelled, the request has no
// counting Decline, and no other offer of the same issuer for the request is stored with another unsigned body.
// It waits for the request and is dropped when any other check fails, the issuer's membership included. An offer
// never counts (Snapshot.ContractCounts).
//
// why one offer per issuer: of two offers from one issuer, the first stored is kept and the second dropped, so
// an issuer cannot widen or renew its answer.
func offerGate(sc *auth.SignedContract, root *astral.Identity, r Reader) user.EntryVerdict {
	req, verdict := contractRequestOf(sc, r)
	if verdict != user.EntryAccept {
		return verdict
	}
	s := r.Snapshot(root)
	stored, _ := s.Request(sc.RequestID)
	switch {
	case !sc.Subject.IsEqual(req.FiledBy), !req.CoversOffer(sc.Contract):
		return user.EntryDrop
	case s.Expelled(sc.Issuer), s.declined(sc.RequestID, stored):
		return user.EntryDrop
	case s.offeredOtherwise(sc):
		return user.EntryDrop
	}
	// why: an offer waits only for its request (Spec: .ai/system/protocols/user/README.md#two-signatures); an
	// issuer with no stored membership may not answer it, so the tier gate must not hold it.
	if _, ok := s.Membership(sc.Issuer); !ok && !IsRoot(root, sc.Issuer) {
		return user.EntryDrop
	}
	return tierGate(r, root, sc.Issuer, func(t user.Tier) bool { return user.MayAnswer(t, req) })
}

// contractRequestOf returns the stored ContractRequest sc names as its RequestID. It waits while the request is
// not stored, and drops sc when RequestID is nil or names another entry.
func contractRequestOf(sc *auth.SignedContract, r Reader) (*user.SignedContractRequest, user.EntryVerdict) {
	if sc.RequestID == nil {
		return nil, user.EntryDrop
	}
	obj, ok := r.Entry(sc.RequestID)
	if !ok {
		return nil, user.EntryWait
	}
	req, ok := obj.(*user.SignedContractRequest)
	if !ok || req.IsNil() {
		return nil, user.EntryDrop
	}
	return req, user.EntryAccept
}

// verify reports whether sig is signer's valid signature of body.
func (c *Rules) verify(signer *astral.Identity, sig *crypto.Signature, body crypto.SignableTextObject) bool {
	if signer.IsZero() || sig == nil {
		return false
	}
	return c.verifier.Verify(secp256k1.FromIdentity(signer), sig, body) == nil
}
