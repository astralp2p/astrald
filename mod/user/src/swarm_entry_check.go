package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// swarmEntryReader is the part of nodes.SwarmState the entry check reads.
type swarmEntryReader interface {
	SwarmEntry(id *astral.ObjectID) (astral.Object, error)
}

// membershipReader is the part of usermod.SwarmView that reads an identity's
// tier.
type membershipReader interface {
	Membership(id *astral.Identity) (*user.Membership, bool)
}

// signatureVerifier is the part of the crypto module the entry check uses.
type signatureVerifier interface {
	Verify(key *crypto.PublicKey, sig *crypto.Signature, obj crypto.SignableTextObject) error
}

// swarmEntryCheck implements usermod.SwarmView's CheckSwarmEntry: which tier
// may write which entry (D2, D5, D7, D10).
//
// note: nothing registers it until G7 composes it into the SwarmView that
// mod/nodes calls on merge.
type swarmEntryCheck struct {
	entries  swarmEntryReader
	members  membershipReader
	verifier signatureVerifier
	root     func() *astral.Identity
}

func newSwarmEntryCheck(entries swarmEntryReader, members membershipReader, verifier signatureVerifier, root func() *astral.Identity) *swarmEntryCheck {
	return &swarmEntryCheck{entries: entries, members: members, verifier: verifier, root: root}
}

// rootKey returns the root key: the user this node is claimed by, or this node
// while it is unclaimed.
func (mod *Module) rootKey() *astral.Identity {
	if id := mod.Identity(); !id.IsZero() {
		return id
	}
	return mod.node.Identity()
}

// entryGate is the identity whose tier decides an entry, and the tiers that
// may write it.
type entryGate struct {
	id     *astral.Identity
	allows func(user.Tier) bool
}

func anyTier(t user.Tier) bool { return t.IsValid() }

func trustedOrAbove(t user.Tier) bool {
	return t == user.TierTrusted || t == user.TierPrivilegedMember
}

// CheckSwarmEntry decides whether obj may enter SwarmState (see
// usermod.SwarmView). Signatures and Timeouts are checked first, then the
// gating identity's tier; the root key writes every kind.
//
// note: Membership excludes an expelled identity, so an entry gated by one
// waits instead of entering.
func (c *swarmEntryCheck) CheckSwarmEntry(obj astral.Object) user.EntryVerdict {
	gate, verdict := c.gateOf(obj)
	if verdict != user.EntryAccept {
		return verdict
	}
	if c.isRoot(gate.id) {
		return user.EntryAccept
	}
	m, ok := c.members.Membership(gate.id)
	if !ok {
		return user.EntryWait
	}
	if !gate.allows(m.Tier) {
		return user.EntryDrop
	}
	return user.EntryAccept
}

// gateOf checks obj's signatures and Timeout and returns its gate. A verdict
// other than EntryAccept is final.
func (c *swarmEntryCheck) gateOf(obj astral.Object) (entryGate, user.EntryVerdict) {
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
		return c.answerGate(e.Issuer, e.RequestID)

	case *user.SignedDecline:
		if e.IsNil() || !c.verify(e.Issuer, e.IssuerSig, e.Decline) {
			return drop()
		}
		return c.answerGate(e.Issuer, e.RequestID)

	case *auth.SignedContract:
		return c.contractGate(e)

	case *user.SignedExpulsion:
		// why: only the user expels (D7).
		if e.Expulsion == nil || !c.isRoot(e.Issuer) || !c.verify(e.Issuer, e.IssuerSig, e.Expulsion) {
			return drop()
		}
		return entryGate{id: e.Issuer}, user.EntryAccept
	}
	return drop()
}

// answerGate gates an Approval or Decline of the request req on its signer's
// tier answering that request (user.MayAnswer). It waits for the request.
func (c *swarmEntryCheck) answerGate(signer *astral.Identity, req *astral.ObjectID) (entryGate, user.EntryVerdict) {
	if req == nil {
		return entryGate{}, user.EntryDrop
	}
	obj, err := c.entries.SwarmEntry(req)
	if err != nil {
		return entryGate{}, user.EntryWait
	}
	if kind, _ := user.EntryKindOf(obj); kind != user.EntryRequests {
		return entryGate{}, user.EntryDrop
	}
	return entryGate{signer, func(t user.Tier) bool { return user.MayAnswer(t, obj) }}, user.EntryAccept
}

// contractGate gates a contract on its issuer, any tier. Its IssuerSig is
// required and its SubjectSig optional; a PrivilegedMembership is issued only
// by the root key (D2).
func (c *swarmEntryCheck) contractGate(sc *auth.SignedContract) (entryGate, user.EntryVerdict) {
	if sc.IsNil() || !c.verify(sc.Issuer, sc.IssuerSig, sc.Contract) {
		return entryGate{}, user.EntryDrop
	}
	if sc.SubjectSig != nil && !c.verify(sc.Subject, sc.SubjectSig, sc.Contract) {
		return entryGate{}, user.EntryDrop
	}
	if tier, ok := user.ContractTier(sc.Contract); ok && tier == user.TierPrivilegedMember && !c.isRoot(sc.Issuer) {
		return entryGate{}, user.EntryDrop
	}
	return entryGate{sc.Issuer, anyTier}, user.EntryAccept
}

func (c *swarmEntryCheck) isRoot(id *astral.Identity) bool {
	root := c.root()
	return !root.IsZero() && id.IsEqual(root)
}

// verify reports whether sig is signer's valid signature of body.
func (c *swarmEntryCheck) verify(signer *astral.Identity, sig *crypto.Signature, body crypto.SignableTextObject) bool {
	if signer.IsZero() || sig == nil {
		return false
	}
	return c.verifier.Verify(secp256k1.FromIdentity(signer), sig, body) == nil
}
