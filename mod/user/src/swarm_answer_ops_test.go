package user

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	secp256k1sig "github.com/astralp2p/astrald/mod/secp256k1"
	"github.com/astralp2p/astrald/mod/user/src/swarmset"
)

// answerFixture is a node at a tier in a swarm whose user key is held on it: the module over a real store with
// the running node's entry check, and a real view. Its requests are filed by members, and their statuses come from
// the entries stored for them.
type answerFixture struct {
	mod     *Module
	auth    *recordingAuth
	crypto  *keyringCrypto
	keys    *joinKeyring
	nodeID  *astral.Identity
	userID  *astral.Identity
	filer   *astral.Identity // a Member, which files contract requests
	carrier *astral.Identity // a Trusted member, which carries joins and files expel requests
	other   *astral.Identity // a Privileged member other than this node
	caller  *astral.Identity
	origin  string // the query's origin; empty is unset

	mu      sync.Mutex
	seeding bool
	written []astral.Object
	// verdict, when set, is the entry check's verdict on every offer.
	verdict user.EntryVerdict
}

// overridingRules is the entry check of an answerFixture: the running node's, except for the verdict a test
// sets on offers.
type overridingRules struct {
	swarmset.Checker
	f *answerFixture
}

func (r overridingRules) Check(obj astral.Object, rd swarmset.Reader) user.EntryVerdict {
	r.f.mu.Lock()
	verdict := r.f.verdict
	r.f.mu.Unlock()
	if sc, ok := obj.(*auth.SignedContract); ok && swarmset.IsOffer(sc) && verdict != "" {
		return verdict
	}
	return r.Checker.Check(obj, rd)
}

func newAnswerFixture(t *testing.T, tier user.Tier) *answerFixture {
	t.Helper()
	keys := newJoinKeyring()
	f := &answerFixture{keys: keys, auth: &recordingAuth{verdict: true}, caller: astral.GenerateIdentity()}
	f.nodeID, f.userID = keys.newKey(), keys.newKey()
	f.filer, f.carrier, f.other = astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()

	f.mod = newStoreModule(t, f.nodeID, f.userID, keys, time.Now, func(root func() *astral.Identity) swarmset.Checker {
		return overridingRules{Checker: realRules(root), f: f}
	})
	f.crypto = f.mod.Crypto.(*keyringCrypto)
	f.mod.Auth = f.auth
	f.mod.writer = &swarmWriter{self: f.nodeID, signer: keys, view: f.mod, entry: f.mod.SwarmEntry, merge: f.mod.swarmState.merge}
	f.mod.carrier = newSwarmCarrier(f.nodeID, f.mod, keys)
	f.mod.swarmState.stored = func(objs []astral.Object) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !f.seeding {
			f.written = append(f.written, objs...)
		}
	}

	valid := time.Now().Add(24 * time.Hour)
	storeMembership(t, f.mod, f.userID, f.filer, user.TierMember, valid)
	storeMembership(t, f.mod, f.userID, f.carrier, user.TierTrusted, valid)
	storeMembership(t, f.mod, f.userID, f.other, user.TierPrivilegedMember, valid)
	if tier != "" {
		storeMembership(t, f.mod, f.userID, f.nodeID, tier, valid)
	}
	f.written = nil
	return f
}

// seed merges the entries the test starts from, which are not what the op wrote.
func (f *answerFixture) seed(t *testing.T, obj astral.Object) *astral.ObjectID {
	t.Helper()
	f.mu.Lock()
	f.seeding = true
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.seeding = false
		f.mu.Unlock()
	}()
	return mergeEntry(t, f.mod, obj)
}

// wrote returns the entries the ops stored.
func (f *answerFixture) wrote() []astral.Object {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]astral.Object(nil), f.written...)
}

func testSig() *crypto.Signature {
	return &crypto.Signature{Scheme: crypto.SchemeASN1, Data: []byte{1}}
}

// contractRequest stores a ContractRequest of the filer, filed at filedAt, and the entries that give it status.
func (f *answerFixture) contractRequest(t *testing.T, filedAt time.Time, status user.RequestStatus) *astral.ObjectID {
	t.Helper()
	body := user.NewContractRequest(f.filer, astral.GenerateIdentity(), []*auth.Permit{{Action: "mod.services.discover_action"}})
	body.FiledAt = astral.Time(filedAt)
	body.Timeout = user.RequestDeadline(astral.Time(filedAt))
	if status == user.RequestExpired {
		body.FiledAt = astral.Time(filedAt.Add(-2 * user.RequestTimeout))
		body.Timeout = user.RequestDeadline(body.FiledAt)
	}
	id := f.seed(t, &user.SignedContractRequest{ContractRequest: body, FiledBySig: testSig()})

	switch status {
	case user.RequestOffered:
		f.seed(t, f.answerContract(body, f.other, id, false))
	case user.RequestFulfilled:
		f.seed(t, f.answerContract(body, f.userID, id, true))
	case user.RequestDeclined:
		f.seed(t, &user.SignedDecline{Decline: user.NewDecline(f.userID, id), IssuerSig: testSig()})
	}
	return id
}

// answerContract returns issuer's contract to the filer of req that answers it: an offer, or when signed both
// parties' contract.
func (f *answerFixture) answerContract(req *user.ContractRequest, issuer *astral.Identity, id *astral.ObjectID, bothSigned bool) *auth.SignedContract {
	var permits []*auth.Permit
	for _, p := range req.Permits {
		deeper := *p
		deeper.Delegation++
		permits = append(permits, &deeper)
	}
	c := &auth.Contract{Issuer: issuer, Subject: req.FiledBy, RequestID: id, Permits: permits, ExpiresAt: astral.Time(time.Now().Add(time.Hour))}
	sc := &auth.SignedContract{Contract: c, IssuerSig: testSig()}
	if bothSigned {
		sc.SubjectSig = testSig()
	}
	return sc
}

func (f *answerFixture) expelRequest(t *testing.T, status user.RequestStatus) *astral.ObjectID {
	t.Helper()
	body := user.NewExpelRequest(f.carrier, astral.GenerateIdentity())
	id := f.seed(t, &user.SignedExpelRequest{ExpelRequest: body, FiledBySig: testSig()})
	if status == user.RequestApproved {
		f.seed(t, &user.SignedApproval{Approval: user.NewApproval(f.userID, id), IssuerSig: testSig()})
	}
	return id
}

func (f *answerFixture) joinRequest(t *testing.T, tier user.Tier) *astral.ObjectID {
	t.Helper()
	body := user.NewJoinRequest(astral.GenerateIdentity(), f.carrier, tier)
	return f.seed(t, &user.SignedJoinRequest{JoinRequest: body, JoinerSig: testSig()})
}

// runOp routes one query to fn as caller and returns every object the op sent.
func runOp(t *testing.T, fn any, caller *astral.Identity, origin, queryString string) []astral.Object {
	t.Helper()
	w := newRecordingWriter()
	if err := routeFrom(t, fn, caller, origin, queryString, w); err != nil {
		t.Fatalf("route: %v", err)
	}
	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never closed the connection")
	}
	w.mu.Lock()
	data := append([]byte(nil), w.buf.Bytes()...)
	w.mu.Unlock()

	var objs []astral.Object
	recv := channel.NewReceiver(bytes.NewReader(data))
	for {
		obj, err := recv.Receive()
		if errors.Is(err, io.EOF) {
			return objs
		}
		if err != nil {
			t.Fatalf("decode answer: %v", err)
		}
		objs = append(objs, obj)
	}
}

func (f *answerFixture) answer(t *testing.T, id *astral.ObjectID, answer string) astral.Object {
	t.Helper()
	return f.answerAs(t, id, answer, "")
}

// answerAs calls user.answer_request with the as argument; empty omits it.
func (f *answerFixture) answerAs(t *testing.T, id *astral.ObjectID, answer, as string) astral.Object {
	t.Helper()
	q := "user.answer_request?id=" + id.String() + "&answer=" + answer
	if as != "" {
		q += "&as=" + as
	}
	objs := runOp(t, f.mod.OpAnswerRequest, f.caller, f.origin, q)
	if len(objs) != 1 {
		t.Fatalf("answer_request sent %d objects; want 1", len(objs))
	}
	return objs[0]
}

// Done: list shows the open requests, pending or offered, with their status.
func TestSwarmRequestsListsPendingWithStatus(t *testing.T) {
	f := newAnswerFixture(t, user.TierPrivilegedMember)
	base := time.Now().Add(-30 * time.Minute)
	later := f.contractRequest(t, base.Add(2*time.Minute), user.RequestPending)
	earlier := f.contractRequest(t, base, user.RequestPending)
	offered := f.contractRequest(t, base.Add(time.Minute), user.RequestOffered)
	fulfilled := f.contractRequest(t, base.Add(3*time.Minute), user.RequestFulfilled)
	expired := f.contractRequest(t, base.Add(-2*time.Hour), user.RequestExpired)

	objs := runOp(t, f.mod.OpSwarmRequests, f.caller, f.origin, "user.swarm_requests")
	assertListed(t, objs, []*astral.ObjectID{earlier, offered, later},
		[]user.RequestStatus{user.RequestPending, user.RequestOffered, user.RequestPending})

	objs = runOp(t, f.mod.OpSwarmRequests, f.caller, f.origin, "user.swarm_requests?all=true")
	assertListed(t, objs, []*astral.ObjectID{expired, earlier, offered, later, fulfilled},
		[]user.RequestStatus{user.RequestExpired, user.RequestPending, user.RequestOffered, user.RequestPending, user.RequestFulfilled})
}

func assertListed(t *testing.T, objs []astral.Object, ids []*astral.ObjectID, statuses []user.RequestStatus) {
	t.Helper()
	if len(objs) != 3*len(ids)+1 {
		t.Fatalf("listed %d objects; want %d", len(objs), 3*len(ids)+1)
	}
	for i, want := range ids {
		id, ok := objs[3*i].(*astral.ObjectID)
		if !ok || !id.IsEqual(want) {
			t.Fatalf("item %d id = %v; want %v", i, objs[3*i], want)
		}
		status, ok := objs[3*i+1].(*user.RequestStatus)
		if !ok || *status != statuses[i] {
			t.Fatalf("item %d status = %v; want %v", i, objs[3*i+1], statuses[i])
		}
		got, err := astral.ResolveObjectID(objs[3*i+2])
		if err != nil || !got.IsEqual(want) {
			t.Fatalf("item %d request has id %v; want %v", i, got, want)
		}
	}
	if _, ok := objs[len(objs)-1].(*astral.EOS); !ok {
		t.Fatalf("list ends with %v; want eos", objs[len(objs)-1])
	}
}

// Done: approving a contract request writes an offer signed by this node
// only: from this node to the filer, every requested permit at one more
// Delegation, the request as its RequestID, expiring with this node's
// membership when that is earlier than 3650 days.
//
// note: ported from TestAnswerRequestWritesSignedApproval; a contract
// request is answered by an offer since the two-signature model.
func TestAnswerRequestWritesAnOffer(t *testing.T) {
	f := newAnswerFixture(t, user.TierPrivilegedMember)
	req := f.contractRequest(t, time.Now(), user.RequestPending)
	obj, _ := f.mod.SwarmEntry(req)
	cr := obj.(*user.SignedContractRequest)

	sc, ok := f.answer(t, req, "approve").(*auth.SignedContract)
	if !ok {
		t.Fatal("answer_request did not send an offer")
	}
	switch {
	case !sc.Issuer.IsEqual(f.nodeID), !sc.Subject.IsEqual(cr.FiledBy), !sc.RequestID.IsEqual(req):
		t.Fatalf("offer from %v to %v for %v; want from this node to the filer for the request", sc.Issuer, sc.Subject, sc.RequestID)
	case sc.SubjectSig != nil:
		t.Fatal("the offer carries a subject signature")
	case !cr.CoversOffer(sc.Contract) || len(sc.Permits) != len(cr.Permits):
		t.Fatalf("offer permits %v; want every requested permit one hop deeper", sc.Permits)
	case sc.ExpiresAt.Time().After(time.Now().Add(offerValidity)):
		t.Fatalf("offer expires %v; want within 3650 days", sc.ExpiresAt)
	}
	f.assertSignedAndWrittenBy(t, f.keys.key(f.nodeID), sc, sc.SignableHash(), sc.IssuerSig)
}

// The offer expires with the issuer's membership when that ends first.
func TestAnswerRequestOfferEndsWithTheMembership(t *testing.T) {
	f := newAnswerFixture(t, "")
	ends := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	storeMembership(t, f.mod, f.userID, f.nodeID, user.TierPrivilegedMember, ends)
	req := f.contractRequest(t, time.Now(), user.RequestPending)

	sc, ok := f.answer(t, req, "approve").(*auth.SignedContract)
	if !ok || !sc.ExpiresAt.Time().Equal(ends) {
		t.Fatalf("answered %v; want an offer expiring with the membership at %v", sc, ends)
	}
}

// Done: approving an expel request writes an Approval signed by this node.
func TestAnswerRequestApprovesAnExpelRequest(t *testing.T) {
	f := newAnswerFixture(t, user.TierPrivilegedMember)
	req := f.expelRequest(t, user.RequestPending)

	sa, ok := f.answer(t, req, "approve").(*user.SignedApproval)
	if !ok {
		t.Fatal("answer_request did not send a signed approval")
	}
	if !sa.Issuer.IsEqual(f.nodeID) || !sa.RequestID.IsEqual(req) {
		t.Fatalf("approval names issuer %v and request %v", sa.Issuer, sa.RequestID)
	}
	f.assertSignedAndWrittenBy(t, f.keys.key(f.nodeID), sa, sa.SignableHash(), sa.IssuerSig)
}

// Done: answer writes a Decline signed by this node, also against an approved
// expel request and an offered contract request.
func TestAnswerRequestWritesSignedDecline(t *testing.T) {
	for name, req := range map[string]func(f *answerFixture) *astral.ObjectID{
		"approved expel request":   func(f *answerFixture) *astral.ObjectID { return f.expelRequest(t, user.RequestApproved) },
		"offered contract request": func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestOffered) },
	} {
		f := newAnswerFixture(t, user.TierPrivilegedMember)
		id := req(f)
		sd, ok := f.answer(t, id, "decline").(*user.SignedDecline)
		if !ok {
			t.Fatalf("%s: answer_request did not send a signed decline", name)
		}
		if !sd.Issuer.IsEqual(f.nodeID) || !sd.RequestID.IsEqual(id) {
			t.Fatalf("%s: decline names issuer %v and request %v", name, sd.Issuer, sd.RequestID)
		}
		f.assertSignedAndWrittenBy(t, f.keys.key(f.nodeID), sd, sd.SignableHash(), sd.IssuerSig)
	}
}

// A held answer (usermod.ErrSwarmEntryHeld) may still be stored, so it is sent;
// a dropped one is sent as an error, not as written.
func TestAnswerRequestSendsHeldAnswer(t *testing.T) {
	f := newAnswerFixture(t, user.TierPrivilegedMember)
	req := f.contractRequest(t, time.Now(), user.RequestPending)
	f.verdict = user.EntryWait
	if _, ok := f.answer(t, req, "approve").(*auth.SignedContract); !ok {
		t.Fatal("a held offer was not sent")
	}

	f = newAnswerFixture(t, user.TierPrivilegedMember)
	req = f.contractRequest(t, time.Now(), user.RequestPending)
	f.verdict = user.EntryDrop
	if obj := f.answer(t, req, "approve"); obj.ObjectType() == (&auth.SignedContract{}).ObjectType() {
		t.Fatal("a dropped offer was sent as written")
	}
}

func (f *answerFixture) assertSignedAndWrittenBy(t *testing.T, key *crypto.PrivateKey, obj astral.Object, hash []byte, sig *crypto.Signature) {
	t.Helper()
	if err := secp256k1sig.VerifyASN1(secp256k1.PublicKey(key), hash, sig); err != nil {
		t.Fatalf("answer is not signed by the expected key: %v", err)
	}
	written := f.wrote()
	if len(written) != 1 {
		t.Fatalf("wrote %d entries; want 1", len(written))
	}
	got, _ := astral.ResolveObjectID(written[0])
	want, _ := astral.ResolveObjectID(obj)
	if !got.IsEqual(want) {
		t.Fatal("the entry written is not the answer sent")
	}
}

// Done: answer refuses outside this node's tier, and writes nothing.
func TestAnswerRequestRefusesOutsideTier(t *testing.T) {
	cases := []struct {
		name   string
		tier   user.Tier
		req    func(f *answerFixture) *astral.ObjectID
		answer string
		want   error
	}{
		{"member declines a join", user.TierMember, func(f *answerFixture) *astral.ObjectID { return f.joinRequest(t, user.TierMember) }, "decline", errOutsideTier},
		{"member declines a contract request", user.TierMember, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestPending) }, "decline", errOutsideTier},
		{"trusted approves a contract request", user.TierTrusted, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestPending) }, "approve", errOutsideTier},
		{"trusted declines a trusted join", user.TierTrusted, func(f *answerFixture) *astral.ObjectID { return f.joinRequest(t, user.TierTrusted) }, "decline", errOutsideTier},
		{"no membership", "", func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestPending) }, "approve", errOutsideTier},
		{"approve an approved expel request", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID { return f.expelRequest(t, user.RequestApproved) }, "approve", errRequestClosed},
		{"approve a fulfilled contract request", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID {
			return f.contractRequest(t, time.Now(), user.RequestFulfilled)
		}, "approve", errRequestClosed},
		{"decline a fulfilled contract request", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID {
			return f.contractRequest(t, time.Now(), user.RequestFulfilled)
		}, "decline", errRequestClosed},
		{"decline an expired request", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestExpired) }, "decline", errRequestClosed},
		{"approve a declined request", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestDeclined) }, "approve", errRequestClosed},
		{"approve a join", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID { return f.joinRequest(t, user.TierMember) }, "approve", errRequestClosed},
		{"unknown request", user.TierPrivilegedMember, func(*answerFixture) *astral.ObjectID { return testObjectID() }, "approve", errUnknownRequest},
		{"invalid answer", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestPending) }, "maybe", errInvalidAnswer},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newAnswerFixture(t, c.tier)
			got := f.answer(t, c.req(f), c.answer)
			if e, ok := got.(astral.Error); !ok || e.Error() != c.want.Error() {
				t.Fatalf("answered %v; want %v", got, c.want)
			}
			if n := len(f.wrote()); n != 0 {
				t.Fatalf("wrote %d entries; want none", n)
			}
		})
	}
}

// An issuer offers at most once per request: the merge check drops a second
// offer of this node with another body, while another issuer may still offer
// for the offered request.
//
// note: offers capped by one membership expire together and have one body, so
// they are one entry; this node's membership here outlasts the offer validity.
func TestAnswerRequestOffersOncePerIssuer(t *testing.T) {
	f := newAnswerFixture(t, user.TierPrivilegedMember)
	storeMembership(t, f.mod, f.userID, f.nodeID, user.TierPrivilegedMember, time.Now().Add(2*offerValidity))
	req := f.contractRequest(t, time.Now(), user.RequestPending)
	if _, ok := f.answer(t, req, "approve").(*auth.SignedContract); !ok {
		t.Fatal("the first offer was not sent")
	}
	if got := f.answer(t, req, "approve"); got.(astral.Error).Error() != errRequestClosed.Error() {
		t.Fatalf("a second offer from this node: %v; want %v", got, errRequestClosed)
	}

	f.caller = f.userID
	sc, ok := f.answerAs(t, req, "approve", "user").(*auth.SignedContract)
	if !ok || !sc.Issuer.IsEqual(f.userID) {
		t.Fatalf("the user key's offer after this node's: %v; want one from the user key", sc)
	}
}

// A Trusted node declines a Member-tier JoinRequest (D5), and approves none:
// its carrier signs the membership.
//
// note: ported from TestAnswerRequestTrustedApprovesMemberJoin; a join has
// no Approval since the two-signature model.
func TestAnswerRequestTrustedDeclinesMemberJoin(t *testing.T) {
	f := newAnswerFixture(t, user.TierTrusted)
	req := f.joinRequest(t, user.TierMember)
	if _, ok := f.answer(t, req, "decline").(*user.SignedDecline); !ok {
		t.Fatal("a trusted node did not decline a member join")
	}
	if got := f.answer(t, req, "approve"); got.(astral.Error).Error() != errRequestClosed.Error() {
		t.Fatalf("a trusted node approving a join: %v; want %v", got, errRequestClosed)
	}
}

// With as=user the user key signs the answer: only for a local query by the
// user, on a node that reaches the user key. Its offer is not capped by a
// membership.
func TestAnswerRequestAsTheUser(t *testing.T) {
	f := newAnswerFixture(t, "")
	req := f.contractRequest(t, time.Now(), user.RequestPending)
	f.caller = f.userID
	sc, ok := f.answerAs(t, req, "approve", "user").(*auth.SignedContract)
	if !ok || !sc.Issuer.IsEqual(f.userID) || sc.SubjectSig != nil {
		t.Fatalf("answered %v; want an offer the user key issued", sc)
	}
	if d := time.Until(sc.ExpiresAt.Time()); d < offerValidity-time.Minute {
		t.Fatalf("the user key's offer expires in %v; want 3650 days", d)
	}
	f.assertSignedAndWrittenBy(t, f.keys.key(f.userID), sc, sc.SignableHash(), sc.IssuerSig)

	for _, c := range []struct {
		name   string
		as     string
		origin string
		caller func(f *answerFixture) *astral.Identity
		noKey  bool
		want   error
	}{
		{"an unknown as", "owner", "", func(f *answerFixture) *astral.Identity { return f.userID }, false, errInvalidAnswer},
		{"over the network", "user", astral.OriginNetwork, func(f *answerFixture) *astral.Identity { return f.userID }, false, astral.NewErrOriginNotAllowed(astral.OriginNetwork)},
		{"without the user key", "user", "", func(f *answerFixture) *astral.Identity { return f.userID }, true, errNoUserKey},
		{"by another caller", "user", "", func(*answerFixture) *astral.Identity { return astral.GenerateIdentity() }, false, auth.ErrNotPermitted},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newAnswerFixture(t, user.TierPrivilegedMember)
			req := f.contractRequest(t, time.Now(), user.RequestPending)
			f.caller, f.origin = c.caller(f), c.origin
			f.crypto.hidden = c.noKey
			got := f.answerAs(t, req, "approve", c.as)
			if e, ok := got.(astral.Error); !ok || e.Error() != c.want.Error() {
				t.Fatalf("answered %v; want %v", got, c.want)
			}
			if n := len(f.wrote()); n != 0 {
				t.Fatalf("wrote %d entries; want none", n)
			}
		})
	}
}

// The caller needs AdminSwarmAction naming the request's subject to answer,
// and SeeSwarmAction to list.
func TestSwarmAnswerOpsAuthorizeCaller(t *testing.T) {
	f := newAnswerFixture(t, user.TierPrivilegedMember)
	req := f.contractRequest(t, time.Now(), user.RequestPending)
	f.auth.verdict = false

	if got := f.answer(t, req, "approve"); got.(astral.Error).Error() != auth.ErrNotPermitted.Error() {
		t.Fatalf("answered %v; want not permitted", got)
	}
	objs := runOp(t, f.mod.OpSwarmRequests, f.caller, f.origin, "user.swarm_requests")
	if len(objs) != 1 || objs[0].(astral.Error).Error() != auth.ErrNotPermitted.Error() {
		t.Fatalf("listed %v; want not permitted", objs)
	}
	if len(f.wrote()) != 0 {
		t.Fatal("a refused caller wrote an entry")
	}

	actions := f.auth.recorded()
	admin, ok := actions[0].(*user.AdminSwarmAction)
	stored, _ := f.mod.SwarmEntry(req)
	if !ok || !admin.Subject.IsEqual(stored.(*user.SignedContractRequest).Identity) {
		t.Fatalf("answer asked %v; want an admin swarm action on the request's identity", actions[0])
	}
	if _, ok := actions[1].(*user.SeeSwarmAction); !ok {
		t.Fatalf("list asked %v; want a see swarm action", actions[1])
	}
}
