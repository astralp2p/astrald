package swarmset

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// keyVerifier accepts a signature whose data is the signer's public key, so a
// fixture signs as an identity with sigBy and a wrong signer fails.
type keyVerifier struct{}

func (keyVerifier) Verify(key *crypto.PublicKey, sig *crypto.Signature, _ crypto.SignableTextObject) error {
	if !bytes.Equal(sig.Data, key.Key) {
		return errors.New("bad signature")
	}
	return nil
}

func sigBy(id *astral.Identity) *crypto.Signature {
	return &crypto.Signature{Scheme: "test", Data: secp256k1.FromIdentity(id).Key}
}

// fakeSwarmSet is a Reader over stored entries and set memberships.
type fakeSwarmSet struct {
	entries map[string]astral.Object
	members map[string]*user.Membership
}

func (f *fakeSwarmSet) Entry(id *astral.ObjectID) (astral.Object, bool) {
	obj, ok := f.entries[id.String()]
	return obj, ok
}

func (f *fakeSwarmSet) Membership(_, id *astral.Identity) (*user.Membership, bool) {
	m, ok := f.members[id.String()]
	return m, ok
}

func (f *fakeSwarmSet) store(t *testing.T, obj astral.Object) *astral.ObjectID {
	t.Helper()
	id, err := astral.ResolveObjectID(obj)
	if err != nil {
		t.Fatalf("resolve entry id: %v", err)
	}
	f.entries[id.String()] = obj
	return id
}

func (f *fakeSwarmSet) setMember(issuer, subject *astral.Identity, tier user.Tier) {
	c := &auth.Contract{Issuer: issuer, Subject: subject, Permits: []*auth.Permit{{Action: astral.String8(tier.Action())}}}
	f.members[subject.String()] = &user.Membership{SignedContract: &auth.SignedContract{Contract: c, IssuerSig: sigBy(issuer)}, Tier: tier}
}

// entryFixture: the root key, a Privileged, a Trusted and a Member node, and a
// stranger with no stored membership.
type entryFixture struct {
	set                                    *fakeSwarmSet
	check                                  *Rules
	root, privileged, trusted, member, out *astral.Identity
}

func newEntryFixture() *entryFixture {
	f := &entryFixture{
		set:  &fakeSwarmSet{entries: map[string]astral.Object{}, members: map[string]*user.Membership{}},
		root: astral.GenerateIdentity(), privileged: astral.GenerateIdentity(),
		trusted: astral.GenerateIdentity(), member: astral.GenerateIdentity(), out: astral.GenerateIdentity(),
	}
	f.set.setMember(f.root, f.privileged, user.TierPrivilegedMember)
	f.set.setMember(f.root, f.trusted, user.TierTrusted)
	f.set.setMember(f.privileged, f.member, user.TierMember)
	f.check = NewRules(keyVerifier{}, func() *astral.Identity { return f.root })
	return f
}

func contractRequest(filedBy *astral.Identity) *user.SignedContractRequest {
	r := user.NewContractRequest(filedBy, astral.GenerateIdentity(), []*auth.Permit{{Action: "test.action"}})
	return &user.SignedContractRequest{ContractRequest: r, FiledBySig: sigBy(filedBy)}
}

func joinRequest(carrier *astral.Identity, tier user.Tier) *user.SignedJoinRequest {
	joiner := astral.GenerateIdentity()
	return &user.SignedJoinRequest{JoinRequest: user.NewJoinRequest(joiner, carrier, tier), JoinerSig: sigBy(joiner)}
}

func expelRequest(filedBy *astral.Identity) *user.SignedExpelRequest {
	r := user.NewExpelRequest(filedBy, astral.GenerateIdentity())
	return &user.SignedExpelRequest{ExpelRequest: r, FiledBySig: sigBy(filedBy)}
}

func approval(signer *astral.Identity, req *astral.ObjectID) *user.SignedApproval {
	return &user.SignedApproval{Approval: user.NewApproval(signer, req), IssuerSig: sigBy(signer)}
}

func decline(signer *astral.Identity, req *astral.ObjectID) *user.SignedDecline {
	return &user.SignedDecline{Decline: user.NewDecline(signer, req), IssuerSig: sigBy(signer)}
}

func contract(issuer, subject *astral.Identity, action string) *auth.SignedContract {
	c := &auth.Contract{
		Issuer: issuer, Subject: subject,
		Permits:   []*auth.Permit{{Action: astral.String8(action)}},
		ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
	}
	return &auth.SignedContract{Contract: c, IssuerSig: sigBy(issuer), SubjectSig: sigBy(subject)}
}

func expulsion(issuer *astral.Identity) *user.SignedExpulsion {
	e := &user.Expulsion{Issuer: issuer, Subject: astral.GenerateIdentity(), ExpelledAt: astral.Now()}
	return &user.SignedExpulsion{Expulsion: e, IssuerSig: sigBy(issuer)}
}

type entryCase struct {
	name string
	obj  astral.Object
	want user.EntryVerdict
}

func runEntryCases(t *testing.T, f *entryFixture, cases []entryCase) {
	t.Helper()
	for _, c := range cases {
		if got := f.check.Check(c.obj, f.set); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestCheckSwarmEntryRequests: any tier files a ContractRequest; only Trusted
// and Privileged carry a JoinRequest or file an ExpelRequest (D5, D10).
func TestCheckSwarmEntryRequests(t *testing.T) {
	f := newEntryFixture()

	badSig := contractRequest(f.member)
	badSig.FiledBySig = sigBy(f.out)
	longTimeout := contractRequest(f.member)
	longTimeout.Timeout = astral.Time(longTimeout.FiledAt.Time().Add(2 * time.Hour))
	badTier := joinRequest(f.trusted, "owner")
	badJoiner := joinRequest(f.trusted, user.TierMember)
	badJoiner.JoinerSig = sigBy(f.trusted)

	runEntryCases(t, f, []entryCase{
		{"contract request by a member", contractRequest(f.member), user.EntryAccept},
		{"contract request by a trusted node", contractRequest(f.trusted), user.EntryAccept},
		{"contract request by a privileged node", contractRequest(f.privileged), user.EntryAccept},
		{"contract request by the root key", contractRequest(f.root), user.EntryAccept},
		{"contract request by a stranger", contractRequest(f.out), user.EntryWait},
		{"contract request, wrong signer", badSig, user.EntryDrop},
		{"contract request, timeout over an hour", longTimeout, user.EntryDrop},

		{"join request carried by a member", joinRequest(f.member, user.TierMember), user.EntryDrop},
		{"join request carried by a trusted node", joinRequest(f.trusted, user.TierMember), user.EntryAccept},
		{"join request carried by a privileged node", joinRequest(f.privileged, user.TierTrusted), user.EntryAccept},
		{"join request carried by the root key", joinRequest(f.root, user.TierPrivilegedMember), user.EntryAccept},
		{"join request carried by a stranger", joinRequest(f.out, user.TierMember), user.EntryWait},
		{"join request at no valid tier", badTier, user.EntryDrop},
		{"join request not signed by its joiner", badJoiner, user.EntryDrop},

		{"expel request by a member", expelRequest(f.member), user.EntryDrop},
		{"expel request by a trusted node", expelRequest(f.trusted), user.EntryAccept},
		{"expel request by a privileged node", expelRequest(f.privileged), user.EntryAccept},
		{"expel request by a stranger", expelRequest(f.out), user.EntryWait},
	})
}

// TestCheckSwarmEntryAnswers: an Approval or Decline enters when its signer's
// tier may answer the request (user.MayAnswer): Privileged any request,
// Trusted a Member-tier JoinRequest only, a Member none (D5).
func TestCheckSwarmEntryAnswers(t *testing.T) {
	f := newEntryFixture()
	cr := f.set.store(t, contractRequest(f.member))
	memberJoin := f.set.store(t, joinRequest(f.trusted, user.TierMember))
	trustedJoin := f.set.store(t, joinRequest(f.privileged, user.TierTrusted))
	expel := f.set.store(t, expelRequest(f.trusted))
	notARequest := f.set.store(t, approval(f.privileged, cr))
	unknown, _ := astral.ResolveObjectID(contractRequest(f.member))

	badSig := approval(f.privileged, cr)
	badSig.IssuerSig = sigBy(f.member)

	runEntryCases(t, f, []entryCase{
		{"approval by a privileged node", approval(f.privileged, cr), user.EntryAccept},
		{"approval of an expel request by a privileged node", approval(f.privileged, expel), user.EntryAccept},
		{"approval by the root key", approval(f.root, cr), user.EntryAccept},
		{"approval by a member", approval(f.member, cr), user.EntryDrop},
		{"approval of a contract request by a trusted node", approval(f.trusted, cr), user.EntryDrop},
		{"approval of a member join by a trusted node", approval(f.trusted, memberJoin), user.EntryAccept},
		{"approval of a trusted join by a trusted node", approval(f.trusted, trustedJoin), user.EntryDrop},
		{"approval of a member join by a member", approval(f.member, memberJoin), user.EntryDrop},
		{"approval by a stranger", approval(f.out, cr), user.EntryWait},
		{"approval of a request not stored yet", approval(f.privileged, unknown), user.EntryWait},
		{"approval of an entry that is no request", approval(f.privileged, notARequest), user.EntryDrop},
		{"approval of no request", approval(f.privileged, nil), user.EntryDrop},
		{"approval, wrong signer", badSig, user.EntryDrop},

		{"decline by a privileged node", decline(f.privileged, cr), user.EntryAccept},
		{"decline by a member", decline(f.member, cr), user.EntryDrop},
		{"decline of a member join by a trusted node", decline(f.trusted, memberJoin), user.EntryAccept},
	})
}

// TestCheckSwarmEntryContractsAndExpels: any member issues a contract, a
// membership only at a tier its own tier issues (MayIssue), and only the root
// key a PrivilegedMembership (D2) and an Expulsion (D7).
func TestCheckSwarmEntryContractsAndExpels(t *testing.T) {
	f := newEntryFixture()
	privileged := user.TierPrivilegedMember.Action()

	noIssuerSig := contract(f.member, astral.GenerateIdentity(), "test.action")
	noIssuerSig.IssuerSig = nil
	noSubjectSig := contract(f.member, f.trusted, "test.action")
	noSubjectSig.SubjectSig = nil
	halfMembership := contract(f.trusted, astral.GenerateIdentity(), user.TierMember.Action())
	halfMembership.SubjectSig = nil
	badSubjectSig := contract(f.member, f.trusted, "test.action")
	badSubjectSig.SubjectSig = sigBy(f.member)
	badExpulsionSig := expulsion(f.root)
	badExpulsionSig.IssuerSig = sigBy(f.privileged)

	runEntryCases(t, f, []entryCase{
		{"app contract by a member", contract(f.member, astral.GenerateIdentity(), "test.action"), user.EntryAccept},
		{"member membership by a trusted carrier", contract(f.trusted, astral.GenerateIdentity(), user.TierMember.Action()), user.EntryAccept},
		{"trusted membership by a trusted carrier", contract(f.trusted, astral.GenerateIdentity(), user.TierTrusted.Action()), user.EntryDrop},
		{"trusted membership by a privileged carrier", contract(f.privileged, astral.GenerateIdentity(), user.TierTrusted.Action()), user.EntryAccept},
		{"member membership by a member", contract(f.member, astral.GenerateIdentity(), user.TierMember.Action()), user.EntryDrop},
		{"member membership by a stranger", contract(f.out, astral.GenerateIdentity(), user.TierMember.Action()), user.EntryWait},
		{"contract by a stranger", contract(f.out, astral.GenerateIdentity(), "test.action"), user.EntryWait},
		{"privileged membership by a privileged node", contract(f.privileged, astral.GenerateIdentity(), privileged), user.EntryDrop},
		{"privileged membership by the root key", contract(f.root, astral.GenerateIdentity(), privileged), user.EntryAccept},
		{"contract with no issuer signature", noIssuerSig, user.EntryDrop},
		{"contract with no subject signature", noSubjectSig, user.EntryDrop},
		{"membership with no subject signature", halfMembership, user.EntryDrop},
		{"contract with a wrong subject signature", badSubjectSig, user.EntryDrop},

		{"expulsion by the root key", expulsion(f.root), user.EntryAccept},
		{"expulsion by a privileged node", expulsion(f.privileged), user.EntryDrop},
		{"expulsion by a stranger", expulsion(f.out), user.EntryDrop},
		{"expulsion, wrong signer", badExpulsionSig, user.EntryDrop},

		{"not an entry", &astral.Ack{}, user.EntryDrop},
		{"unsigned request body", contractRequest(f.member).ContractRequest, user.EntryDrop},
	})
}

// TestCheckSwarmEntryReadsTheRootOnce: a claim that changes the root key
// while an Expulsion is checked does not split the check between two roots,
// even when the old root key holds a membership under the new one.
func TestCheckSwarmEntryReadsTheRootOnce(t *testing.T) {
	f := newEntryFixture()
	claimed := astral.GenerateIdentity()
	f.set.setMember(claimed, f.root, user.TierPrivilegedMember)
	reads := 0
	f.check = NewRules(keyVerifier{}, func() *astral.Identity {
		reads++
		if reads == 1 {
			return f.root
		}
		return claimed
	})

	if got := f.check.Check(expulsion(f.root), f.set); got != user.EntryAccept {
		t.Fatalf("expulsion by the root read at the start: got %v, want accept", got)
	}
	if reads != 1 {
		t.Fatalf("root read %d times, want 1", reads)
	}
}

// TestCheckSwarmEntryUnclaimedNodeIsItsOwnRoot: before a claim the node's own
// key, as the root key, writes every kind.
func TestCheckSwarmEntryUnclaimedNodeIsItsOwnRoot(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	f := newEntryFixture()
	f.check = NewRules(keyVerifier{}, func() *astral.Identity { return nodeID })
	runEntryCases(t, f, []entryCase{
		{"expulsion by the unclaimed node", expulsion(nodeID), user.EntryAccept},
		{"privileged membership by the unclaimed node", contract(nodeID, astral.GenerateIdentity(), user.TierPrivilegedMember.Action()), user.EntryAccept},
	})
}
