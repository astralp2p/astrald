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
//   - Approval, Decline: the signer, whose tier must answer the request named by RequestID (user.MayAnswer);
//   - contract: the Issuer, any tier; a membership: the Issuer, whose tier must issue the membership's tier
//     (MayIssue), so a membership at user.TierPrivilegedMember is issued only by the root key (D2);
//   - Expulsion: only the root key (D7).
//
// The root key needs no membership and may write every kind. Check returns:
//   - user.EntryDrop: obj is not an entry, a signature does not verify (a contract's IssuerSig is required, its
//     SubjectSig optional), a request's Timeout is not valid, or the gating identity's tier may not write it;
//   - user.EntryWait: the gating identity's membership, or the request an Approval or Decline names, is not
//     stored yet;
//   - user.EntryAccept: otherwise.
//
// An entry that was valid when it arrived stays valid; expels apply when entries are read, not here.
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

// entryGate is the identity whose tier decides an entry, and the tiers that may write it.
type entryGate struct {
	id     *astral.Identity
	allows func(user.Tier) bool
}

func anyTier(t user.Tier) bool { return t.IsValid() }

// noTier gates what only the root key writes.
func noTier(user.Tier) bool { return false }

func trustedOrAbove(t user.Tier) bool {
	return t == user.TierTrusted || t == user.TierPrivilegedMember
}

// Check decides whether obj may enter the set. Signatures and Timeouts are checked first, then the gating
// identity's tier; the root key writes every kind.
//
// note: Membership excludes an expelled identity, so an entry gated by one waits instead of entering.
//
// why the root is read once: a claim can change it between two reads, and the gate must be judged against the
// root it was built for.
func (c *Rules) Check(obj astral.Object, r Reader) user.EntryVerdict {
	root := c.root()
	gate, verdict := c.gateOf(obj, root, r)
	if verdict != user.EntryAccept {
		return verdict
	}
	if IsRoot(root, gate.id) {
		return user.EntryAccept
	}
	m, ok := r.Membership(root, gate.id)
	if !ok {
		return user.EntryWait
	}
	if !gate.allows(m.Tier) {
		return user.EntryDrop
	}
	return user.EntryAccept
}

// gateOf checks obj's signatures and Timeout and returns its gate. A verdict other than EntryAccept is final.
func (c *Rules) gateOf(obj astral.Object, root *astral.Identity, r Reader) (entryGate, user.EntryVerdict) {
	drop := func() (entryGate, user.EntryVerdict) { return entryGate{}, user.EntryDrop }

	switch e := obj.(type) {
	case *user.SignedContractRequest:
		if e.IsNil() || !e.ValidTimeout() || !c.verify(e.FiledBy, e.FiledBySig, e.ContractRequest) {
			return drop()
		}
		return entryGate{e.FiledBy, anyTier}, user.EntryAccept

	case *user.SignedJoinRequest:
		if e.IsNil() || !e.ValidTimeout() || !e.Tier.IsValid() || !c.verify(e.Joiner, e.JoinerSig, e.JoinRequest) {
			return drop()
		}
		return entryGate{e.Carrier, trustedOrAbove}, user.EntryAccept

	case *user.SignedExpelRequest:
		if e.IsNil() || !e.ValidTimeout() || !c.verify(e.FiledBy, e.FiledBySig, e.ExpelRequest) {
			return drop()
		}
		return entryGate{e.FiledBy, trustedOrAbove}, user.EntryAccept

	case *user.SignedApproval:
		if e.IsNil() || !c.verify(e.Issuer, e.IssuerSig, e.Approval) {
			return drop()
		}
		return answerGate(e.Issuer, e.RequestID, r)

	case *user.SignedDecline:
		if e.IsNil() || !c.verify(e.Issuer, e.IssuerSig, e.Decline) {
			return drop()
		}
		return answerGate(e.Issuer, e.RequestID, r)

	case *auth.SignedContract:
		return c.contractGate(e, root)

	case *user.SignedExpulsion:
		// why: only the user expels (D7).
		if e.Expulsion == nil || !IsRoot(root, e.Issuer) || !c.verify(e.Issuer, e.IssuerSig, e.Expulsion) {
			return drop()
		}
		return entryGate{e.Issuer, noTier}, user.EntryAccept
	}
	return drop()
}

// answerGate gates an Approval or Decline of the request req on its signer's tier answering that request
// (user.MayAnswer). It waits for the request.
func answerGate(signer *astral.Identity, req *astral.ObjectID, r Reader) (entryGate, user.EntryVerdict) {
	if req == nil {
		return entryGate{}, user.EntryDrop
	}
	obj, ok := r.Entry(req)
	if !ok {
		return entryGate{}, user.EntryWait
	}
	if kind, _ := user.EntryKindOf(obj); kind != user.EntryRequests {
		return entryGate{}, user.EntryDrop
	}
	return entryGate{signer, func(t user.Tier) bool { return user.MayAnswer(t, obj) }}, user.EntryAccept
}

// contractGate gates a contract on its issuer: any tier, and for a membership a tier that issues the
// membership's tier (MayIssue). Its IssuerSig is required and its SubjectSig optional; a PrivilegedMembership
// is issued only by the root key (D2).
func (c *Rules) contractGate(sc *auth.SignedContract, root *astral.Identity) (entryGate, user.EntryVerdict) {
	if sc.IsNil() || !c.verify(sc.Issuer, sc.IssuerSig, sc.Contract) {
		return entryGate{}, user.EntryDrop
	}
	if sc.SubjectSig != nil && !c.verify(sc.Subject, sc.SubjectSig, sc.Contract) {
		return entryGate{}, user.EntryDrop
	}
	tier, ok := user.ContractTier(sc.Contract)
	if !ok {
		return entryGate{sc.Issuer, anyTier}, user.EntryAccept
	}
	if tier == user.TierPrivilegedMember && !IsRoot(root, sc.Issuer) {
		return entryGate{}, user.EntryDrop
	}
	return entryGate{sc.Issuer, func(issuer user.Tier) bool { return MayIssue(issuer, tier) }}, user.EntryAccept
}

// verify reports whether sig is signer's valid signature of body.
func (c *Rules) verify(signer *astral.Identity, sig *crypto.Signature, body crypto.SignableTextObject) bool {
	if signer.IsZero() || sig == nil {
		return false
	}
	return c.verifier.Verify(secp256k1.FromIdentity(signer), sig, body) == nil
}
