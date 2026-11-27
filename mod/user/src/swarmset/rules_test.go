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

// Snapshot reads the stored entries; its memberships are the set's own, not the fake's.
func (f *fakeSwarmSet) Snapshot(root *astral.Identity) *Snapshot {
	x := NewIndex()
	for _, obj := range f.entries {
		id, _ := astral.ResolveObjectID(obj)
		x.Add(id, obj)
	}
	return &Snapshot{Index: x, Root: root, Now: time.Now()}
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

// TestCheckSwarmEntryAnswers: an Approval enters only for an ExpelRequest, a
// Decline for any request, each when its signer's tier may answer the request
// (user.MayAnswer): Privileged any request, Trusted a Member-tier JoinRequest
// only, a Member none (D5).
//
// note: the Approval rows of a ContractRequest and a JoinRequest now drop: a
// contract request is answered by an offer and a join by its carrier's
// signature; their tier rows moved to the Decline rows.
func TestCheckSwarmEntryAnswers(t *testing.T) {
	f := newEntryFixture()
	cr := f.set.store(t, contractRequest(f.member))
	memberJoin := f.set.store(t, joinRequest(f.trusted, user.TierMember))
	trustedJoin := f.set.store(t, joinRequest(f.privileged, user.TierTrusted))
	expel := f.set.store(t, expelRequest(f.trusted))
	notARequest := f.set.store(t, decline(f.privileged, cr))
	unknown, _ := astral.ResolveObjectID(expelRequest(f.trusted))

	badSig := approval(f.privileged, expel)
	badSig.IssuerSig = sigBy(f.member)

	runEntryCases(t, f, []entryCase{
		{"approval of an expel request by a privileged node", approval(f.privileged, expel), user.EntryAccept},
		{"approval of an expel request by the root key", approval(f.root, expel), user.EntryAccept},
		{"approval of an expel request by a trusted node", approval(f.trusted, expel), user.EntryDrop},
		{"approval of an expel request by a member", approval(f.member, expel), user.EntryDrop},
		{"approval of an expel request by a stranger", approval(f.out, expel), user.EntryWait},
		{"approval of a contract request", approval(f.privileged, cr), user.EntryDrop},
		{"approval of a contract request by the root key", approval(f.root, cr), user.EntryDrop},
		{"approval of a member join", approval(f.trusted, memberJoin), user.EntryDrop},
		{"approval of a request not stored yet", approval(f.privileged, unknown), user.EntryWait},
		{"approval of an entry that is no request", approval(f.privileged, notARequest), user.EntryDrop},
		{"approval of no request", approval(f.privileged, nil), user.EntryDrop},
		{"approval, wrong signer", badSig, user.EntryDrop},

		{"decline by a privileged node", decline(f.privileged, cr), user.EntryAccept},
		{"decline by the root key", decline(f.root, cr), user.EntryAccept},
		{"decline by a member", decline(f.member, cr), user.EntryDrop},
		{"decline of a contract request by a trusted node", decline(f.trusted, cr), user.EntryDrop},
		{"decline of a member join by a trusted node", decline(f.trusted, memberJoin), user.EntryAccept},
		{"decline of a trusted join by a trusted node", decline(f.trusted, trustedJoin), user.EntryDrop},
		{"decline of a member join by a member", decline(f.member, memberJoin), user.EntryDrop},
		{"decline by a stranger", decline(f.out, cr), user.EntryWait},
		{"decline of an entry that is no request", decline(f.privileged, notARequest), user.EntryDrop},
	})
}

// offer returns issuer's offer, signed by issuer only, that answers the stored
// ContractRequest req with entry ID reqID: every requested permit at one more
// Delegation, to req's filer.
func offer(issuer *astral.Identity, req *user.SignedContractRequest, reqID *astral.ObjectID) *auth.SignedContract {
	var permits []*auth.Permit
	for _, p := range req.Permits {
		deeper := *p
		deeper.Delegation++
		permits = append(permits, &deeper)
	}
	c := &auth.Contract{
		Issuer: issuer, Subject: req.FiledBy, Permits: permits, RequestID: reqID,
		ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
	}
	return &auth.SignedContract{Contract: c, IssuerSig: sigBy(issuer)}
}

// completed returns sc with its subject's signature added.
func completed(sc *auth.SignedContract) *auth.SignedContract {
	return &auth.SignedContract{Contract: sc.Contract, IssuerSig: sc.IssuerSig, SubjectSig: sigBy(sc.Subject)}
}

// appContract returns the filer's contract to the app req names, signed by
// both, with the requested permits.
func appContract(req *user.SignedContractRequest, reqID *astral.ObjectID) *auth.SignedContract {
	c := &auth.Contract{
		Issuer: req.FiledBy, Subject: req.Identity, Permits: req.Permits, RequestID: reqID,
		ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
	}
	return &auth.SignedContract{Contract: c, IssuerSig: sigBy(req.FiledBy), SubjectSig: sigBy(req.Identity)}
}

// TestCheckSwarmEntryContractsAndExpels: a membership enters only at a tier its
// issuer's tier issues (MayIssue), and only the root key issues a
// PrivilegedMembership (D2) and an Expulsion (D7). Any other contract signed by
// both enters only when the root key issued it, or when it names a stored
// ContractRequest: the approver's contract to the filer, from a tier that
// answers the request, or the filer's contract to the app with requested
// permits.
func TestCheckSwarmEntryContractsAndExpels(t *testing.T) {
	f := newEntryFixture()
	privileged := user.TierPrivilegedMember.Action()
	req := contractRequest(f.member)
	reqID := f.set.store(t, req)
	strangers := contractRequest(f.out)
	strangersID := f.set.store(t, strangers)
	expelID := f.set.store(t, expelRequest(f.trusted))
	unstored := contractRequest(f.member)
	unstoredID, _ := astral.ResolveObjectID(unstored)

	noIssuerSig := contract(f.member, astral.GenerateIdentity(), "test.action")
	noIssuerSig.IssuerSig = nil
	halfMembership := contract(f.trusted, astral.GenerateIdentity(), user.TierMember.Action())
	halfMembership.SubjectSig = nil
	badSubjectSig := appContract(req, reqID)
	badSubjectSig.SubjectSig = sigBy(f.member)
	wider := appContract(req, reqID)
	wider.Permits = append(wider.Permits, &auth.Permit{Action: "other.action"})
	toAnother := appContract(req, reqID)
	toAnother.Subject = astral.GenerateIdentity()
	toAnother.SubjectSig = sigBy(toAnother.Subject)
	noRequest := appContract(req, nil)
	ofExpel := appContract(req, expelID)
	badExpulsionSig := expulsion(f.root)
	badExpulsionSig.IssuerSig = sigBy(f.privileged)

	runEntryCases(t, f, []entryCase{
		{"member membership by a trusted carrier", contract(f.trusted, astral.GenerateIdentity(), user.TierMember.Action()), user.EntryAccept},
		{"trusted membership by a trusted carrier", contract(f.trusted, astral.GenerateIdentity(), user.TierTrusted.Action()), user.EntryDrop},
		{"trusted membership by a privileged carrier", contract(f.privileged, astral.GenerateIdentity(), user.TierTrusted.Action()), user.EntryAccept},
		{"member membership by a member", contract(f.member, astral.GenerateIdentity(), user.TierMember.Action()), user.EntryDrop},
		{"member membership by a stranger", contract(f.out, astral.GenerateIdentity(), user.TierMember.Action()), user.EntryWait},
		{"privileged membership by a privileged node", contract(f.privileged, astral.GenerateIdentity(), privileged), user.EntryDrop},
		{"privileged membership by the root key", contract(f.root, astral.GenerateIdentity(), privileged), user.EntryAccept},
		{"membership with no subject signature", halfMembership, user.EntryDrop},

		{"host's contract to its app", appContract(req, reqID), user.EntryAccept},
		{"approver's completed contract to the host", completed(offer(f.privileged, req, reqID)), user.EntryAccept},
		{"root key's completed contract to the host", completed(offer(f.root, req, reqID)), user.EntryAccept},
		{"trusted node's completed contract to the host", completed(offer(f.trusted, req, reqID)), user.EntryDrop},
		{"contract the root key issued, naming no request", contract(f.root, astral.GenerateIdentity(), "test.action"), user.EntryAccept},
		{"contract naming no request", noRequest, user.EntryDrop},
		{"contract naming an expel request", ofExpel, user.EntryDrop},
		{"contract naming a request not stored yet", appContract(unstored, unstoredID), user.EntryWait},
		{"contract wider than its request", wider, user.EntryDrop},
		{"contract to another identity than the app", toAnother, user.EntryDrop},
		{"contract by a stranger to its app", appContract(strangers, strangersID), user.EntryWait},
		{"contract with no issuer signature", noIssuerSig, user.EntryDrop},
		{"contract with a wrong subject signature", badSubjectSig, user.EntryDrop},

		{"expulsion by the root key", expulsion(f.root), user.EntryAccept},
		{"expulsion by a privileged node", expulsion(f.privileged), user.EntryDrop},
		{"expulsion by a stranger", expulsion(f.out), user.EntryDrop},
		{"expulsion, wrong signer", badExpulsionSig, user.EntryDrop},

		{"not an entry", &astral.Ack{}, user.EntryDrop},
		{"unsigned request body", contractRequest(f.member).ContractRequest, user.EntryDrop},
	})
}

// TestCheckSwarmEntryOffers (criteria 15, 18): an offer, a contract only its
// issuer signed, enters only when it names a stored ContractRequest filed by
// its subject, is no wider than the request (the offer depth rule), its issuer
// may answer the request and is not expelled, the request has no Decline, and
// the issuer has stored no other offer for it. It waits for its request.
func TestCheckSwarmEntryOffers(t *testing.T) {
	f := newEntryFixture()
	req := contractRequest(f.member)
	reqID := f.set.store(t, req)
	unstored := contractRequest(f.member)
	unstoredID, _ := astral.ResolveObjectID(unstored)
	declined := contractRequest(f.member)
	declinedID := f.set.store(t, declined)
	f.set.store(t, decline(f.privileged, declinedID))
	expelID := f.set.store(t, expelRequest(f.trusted))
	expelledIssuer := astral.GenerateIdentity()
	f.set.setMember(f.root, expelledIssuer, user.TierPrivilegedMember)
	f.set.store(t, &user.SignedExpulsion{
		Expulsion: &user.Expulsion{Issuer: f.root, Subject: expelledIssuer, ExpelledAt: astral.Now()},
		IssuerSig: sigBy(f.root),
	})
	renewed := contractRequest(f.member)
	renewedID := f.set.store(t, renewed)
	first := offer(f.privileged, renewed, renewedID)
	f.set.store(t, first)

	toAnother := offer(f.privileged, req, reqID)
	toAnother.Subject = f.trusted
	sameDepth := offer(f.privileged, req, reqID)
	sameDepth.Permits[0].Delegation = 0
	tooDeep := offer(f.privileged, req, reqID)
	tooDeep.Permits[0].Delegation = 2
	wider := offer(f.privileged, req, reqID)
	wider.Permits = append(wider.Permits, &auth.Permit{Action: "other.action", Delegation: 1})
	repeated := offer(f.privileged, req, reqID)
	repeated.Permits = append(repeated.Permits, repeated.Permits[0])
	ofExpel := offer(f.privileged, req, expelID)
	second := offer(f.privileged, renewed, renewedID)
	second.ExpiresAt = astral.Time(time.Now().Add(2 * time.Hour))
	badSig := offer(f.privileged, req, reqID)
	badSig.IssuerSig = sigBy(f.member)

	runEntryCases(t, f, []entryCase{
		{"offer by a privileged node", offer(f.privileged, req, reqID), user.EntryAccept},
		{"offer by the root key", offer(f.root, req, reqID), user.EntryAccept},
		{"offer by a trusted node", offer(f.trusted, req, reqID), user.EntryDrop},
		{"offer by a member", offer(f.member, req, reqID), user.EntryDrop},
		{"offer by a stranger", offer(f.out, req, reqID), user.EntryDrop},
		{"offer by an expelled node", offer(expelledIssuer, req, reqID), user.EntryDrop},
		{"offer before its request", offer(f.privileged, unstored, unstoredID), user.EntryWait},
		{"offer naming no request", offer(f.privileged, req, nil), user.EntryDrop},
		{"offer naming an expel request", ofExpel, user.EntryDrop},
		{"offer to another node than the filer", toAnother, user.EntryDrop},
		{"offer at the requested depth", sameDepth, user.EntryDrop},
		{"offer two hops deeper", tooDeep, user.EntryDrop},
		{"offer wider than the request", wider, user.EntryDrop},
		{"offer repeating a permit", repeated, user.EntryDrop},
		{"offer for a declined request", offer(f.privileged, declined, declinedID), user.EntryDrop},
		{"the same offer again", first, user.EntryAccept},
		{"a second offer from one issuer", second, user.EntryDrop},
		{"a second issuer's offer", offer(f.root, renewed, renewedID), user.EntryAccept},
		{"offer, wrong signer", badSig, user.EntryDrop},
	})

	// why: an offer may carry only some of the requested permits.
	two := contractRequest(f.member)
	two.Permits = append(two.Permits, &auth.Permit{Action: "other.action"})
	twoID := f.set.store(t, two)
	subset := offer(f.privileged, two, twoID)
	subset.Permits = subset.Permits[:1]
	runEntryCases(t, f, []entryCase{{"offer of some requested permits", subset, user.EntryAccept}})
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
