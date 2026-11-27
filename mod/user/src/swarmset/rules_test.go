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

// entryFixture is the real Set the entry check reads: the root key, a Privileged, a Trusted and a Member node,
// each with a stored membership, and a stranger with none.
type entryFixture struct {
	*setFixture
	privileged, member, out *astral.Identity
}

func newEntryFixture(t *testing.T) *entryFixture {
	f := &entryFixture{
		setFixture: newSetFixture(t),
		privileged: astral.GenerateIdentity(), member: astral.GenerateIdentity(), out: astral.GenerateIdentity(),
	}
	f.mustAdd(f.membership(f.root, f.privileged, user.TierPrivilegedMember, time.Hour))
	f.mustAdd(f.membership(f.privileged, f.member, user.TierMember, time.Hour))
	return f
}

// store adds obj to the set, which must accept it, and returns its entry ID.
func (f *entryFixture) store(obj astral.Object) *astral.ObjectID {
	f.t.Helper()
	f.mustAdd(obj)
	return entryIDOf(f.t, obj)
}

// verdict returns the entry check's verdict on obj as the set's stored entries stand now.
func (f *entryFixture) verdict(obj astral.Object) user.EntryVerdict {
	return f.check.Check(obj, f.set.reader(time.Now()))
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

// TestCheckSwarmEntryRequests: any tier files a ContractRequest; only Trusted
// and Privileged carry a JoinRequest or file an ExpelRequest (D5, D10).
func TestCheckSwarmEntryRequests(t *testing.T) {
	f := newEntryFixture(t)

	badSig := contractRequest(f.member)
	badSig.FiledBySig = sigBy(f.out)
	longTimeout := contractRequest(f.member)
	longTimeout.Timeout = astral.Time(longTimeout.FiledAt.Time().Add(2 * time.Hour))
	badTier := joinRequest(f.trusted, "owner")
	badJoiner := joinRequest(f.trusted, user.TierMember)
	badJoiner.JoinerSig = sigBy(f.trusted)

	for _, tc := range []struct {
		name string
		obj  astral.Object
		want user.EntryVerdict
	}{
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := f.verdict(tc.obj); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
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
	f := newEntryFixture(t)
	cr := f.store(contractRequest(f.member))
	memberJoin := f.store(joinRequest(f.trusted, user.TierMember))
	trustedJoin := f.store(joinRequest(f.privileged, user.TierTrusted))
	expel := f.store(expelRequest(f.trusted))
	notARequest := f.store(decline(f.privileged, cr))
	unknown, _ := astral.ResolveObjectID(expelRequest(f.trusted))

	badSig := approval(f.privileged, expel)
	badSig.IssuerSig = sigBy(f.member)

	for _, tc := range []struct {
		name string
		obj  astral.Object
		want user.EntryVerdict
	}{
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := f.verdict(tc.obj); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
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
	f := newEntryFixture(t)
	privileged := user.TierPrivilegedMember.Action()
	req := contractRequest(f.member)
	reqID := f.store(req)
	expelID := f.store(expelRequest(f.trusted))
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

	for _, tc := range []struct {
		name string
		obj  astral.Object
		want user.EntryVerdict
	}{
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
		{"contract with no issuer signature", noIssuerSig, user.EntryDrop},
		{"contract with a wrong subject signature", badSubjectSig, user.EntryDrop},

		{"expulsion by the root key", expulsion(f.root), user.EntryAccept},
		{"expulsion by a privileged node", expulsion(f.privileged), user.EntryDrop},
		{"expulsion by a stranger", expulsion(f.out), user.EntryDrop},
		{"expulsion, wrong signer", badExpulsionSig, user.EntryDrop},

		{"not an entry", &astral.Ack{}, user.EntryDrop},
		{"unsigned request body", contractRequest(f.member).ContractRequest, user.EntryDrop},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := f.verdict(tc.obj); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCheckSwarmEntryOffers (criteria 15, 18): an offer, a contract only its
// issuer signed, enters only when it names a stored ContractRequest filed by
// its subject, is no wider than the request (the offer depth rule), its issuer
// may answer the request and is not expelled, the request has no Decline, and
// the issuer has stored no other offer for it. It waits for its request.
func TestCheckSwarmEntryOffers(t *testing.T) {
	f := newEntryFixture(t)
	req := contractRequest(f.member)
	reqID := f.store(req)
	unstored := contractRequest(f.member)
	unstoredID, _ := astral.ResolveObjectID(unstored)
	declined := contractRequest(f.member)
	declinedID := f.store(declined)
	f.store(decline(f.privileged, declinedID))
	expelID := f.store(expelRequest(f.trusted))
	expelledIssuer := astral.GenerateIdentity()
	f.mustAdd(f.membership(f.root, expelledIssuer, user.TierPrivilegedMember, time.Hour))
	f.store(&user.SignedExpulsion{
		Expulsion: &user.Expulsion{Issuer: f.root, Subject: expelledIssuer, ExpelledAt: astral.Now()},
		IssuerSig: sigBy(f.root),
	})
	renewed := contractRequest(f.member)
	renewedID := f.store(renewed)
	first := offer(f.privileged, renewed, renewedID)
	f.store(first)

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

	for _, tc := range []struct {
		name string
		obj  astral.Object
		want user.EntryVerdict
	}{
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := f.verdict(tc.obj); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}

	// why: an offer may carry only some of the requested permits.
	two := contractRequest(f.member)
	two.Permits = append(two.Permits, &auth.Permit{Action: "other.action"})
	twoID := f.store(two)
	subset := offer(f.privileged, two, twoID)
	subset.Permits = subset.Permits[:1]
	for _, tc := range []struct {
		name string
		obj  astral.Object
		want user.EntryVerdict
	}{
		{"offer of some requested permits", subset, user.EntryAccept},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := f.verdict(tc.obj); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCheckSwarmEntryDeclineClosesOnlyTheRequest (criterion 18): once a
// Decline names a request, the merge check drops every new offer for it, from
// any issuer, while the completed form of an offer stored before the Decline
// and the host's contract to its app, both signed by both parties, still
// enter.
func TestCheckSwarmEntryDeclineClosesOnlyTheRequest(t *testing.T) {
	f := newEntryFixture()
	req := contractRequest(f.member)
	reqID := f.set.store(t, req)
	stored := offer(f.privileged, req, reqID)
	f.set.store(t, stored)
	f.set.store(t, decline(f.trusted, reqID))
	other := astral.GenerateIdentity()
	f.set.setMember(f.root, other, user.TierPrivilegedMember)

	runEntryCases(t, f, []entryCase{
		{"a new offer by the root key", offer(f.root, req, reqID), user.EntryDrop},
		{"a new offer by another privileged node", offer(other, req, reqID), user.EntryDrop},
		{"the completed form of the stored offer", completed(stored), user.EntryAccept},
		{"the host's contract to its app", appContract(req, reqID), user.EntryAccept},
	})
}

// TestCheckSwarmEntryReadsTheRootOnce: a claim that changes the root key
// while an Expulsion is checked does not split the check between two roots,
// even when the old root key holds a membership under the new one.
func TestCheckSwarmEntryReadsTheRootOnce(t *testing.T) {
	f := newEntryFixture(t)
	oldRoot, claimed := f.root, astral.GenerateIdentity()
	f.root = claimed
	f.mustAdd(f.membership(claimed, oldRoot, user.TierPrivilegedMember, time.Hour))
	reads := 0
	f.check = NewRules(keyVerifier{}, func() *astral.Identity {
		reads++
		if reads == 1 {
			return oldRoot
		}
		return claimed
	})

	if got := f.verdict(expulsion(oldRoot)); got != user.EntryAccept {
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
	f := newEntryFixture(t)
	f.check = NewRules(keyVerifier{}, func() *astral.Identity { return nodeID })
	for _, tc := range []struct {
		name string
		obj  astral.Object
		want user.EntryVerdict
	}{
		{"expulsion by the unclaimed node", expulsion(nodeID), user.EntryAccept},
		{"privileged membership by the unclaimed node", contract(nodeID, astral.GenerateIdentity(), user.TierPrivilegedMember.Action()), user.EntryAccept},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := f.verdict(tc.obj); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
