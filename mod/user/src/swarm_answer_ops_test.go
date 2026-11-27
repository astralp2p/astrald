package user

import (
	"bytes"
	"errors"
	"fmt"
	usermod "github.com/astralp2p/astrald/mod/user"
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
)

// answerSwarmState is an in-memory SwarmState store: entries keyed by entry
// ID. addErr, when set, is returned by merge after it records the entry.
type answerSwarmState struct {
	mu      sync.Mutex
	entries map[string]astral.Object
	ids     map[string]*astral.ObjectID
	added   []astral.Object
	addErr  error
}

func newAnswerSwarmState() *answerSwarmState {
	return &answerSwarmState{entries: map[string]astral.Object{}, ids: map[string]*astral.ObjectID{}}
}

func (s *answerSwarmState) store(t *testing.T, obj astral.Object) *astral.ObjectID {
	t.Helper()
	id, err := astral.ResolveObjectID(obj)
	if err != nil {
		t.Fatalf("resolve id: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[id.String()] = obj
	s.ids[id.String()] = id
	return id
}

// merge is the store's merge, for the typed writes of a swarmWriter.
func (s *answerSwarmState) merge(_ *astral.Context, obj astral.Object) error {
	id, err := astral.ResolveObjectID(obj)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[id.String()] = obj
	s.ids[id.String()] = id
	s.added = append(s.added, obj)
	return s.addErr
}

func (s *answerSwarmState) SwarmEntry(id *astral.ObjectID) (astral.Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.entries[id.String()]
	if !ok {
		return nil, errors.New("not stored")
	}
	return obj, nil
}

func (s *answerSwarmState) SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []*astral.ObjectID
	for key, obj := range s.entries {
		if k, ok := user.EntryKindOf(obj); ok && k == kind {
			ids = append(ids, s.ids[key])
		}
	}
	return ids, nil
}

func (s *answerSwarmState) SwarmHead() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return uint64(len(s.entries))
}

func (s *answerSwarmState) written() []astral.Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]astral.Object(nil), s.added...)
}

// fakeSwarmView answers statuses and tiers from maps, MayAnswer by the tier
// over the requests state stores (the root key answers every one), and
// ContractsOf from the contracts state stores; every other method returns
// nothing.
type fakeSwarmView struct {
	status map[string]user.RequestStatus
	tiers  map[string]user.Tier
	state  *answerSwarmState
	root   *astral.Identity
}

func (v *fakeSwarmView) RequestStatus(req *astral.ObjectID) user.RequestStatus {
	if s, ok := v.status[req.String()]; ok {
		return s
	}
	return user.RequestPending
}
func (v *fakeSwarmView) Expelled(*astral.Identity) bool                            { return false }
func (v *fakeSwarmView) ApprovalsTouching(*astral.Identity) []*user.SignedApproval { return nil }
func (v *fakeSwarmView) ContractsOf(req *astral.ObjectID) (list []*auth.SignedContract) {
	ids, _ := v.state.SwarmEntries(user.EntryContracts)
	for _, id := range ids {
		obj, _ := v.state.SwarmEntry(id)
		if sc, ok := obj.(*auth.SignedContract); ok && sc.RequestID.IsEqual(req) {
			list = append(list, sc)
		}
	}
	return list
}
func (v *fakeSwarmView) MayAnswer(signer *astral.Identity, req *astral.ObjectID) bool {
	obj, err := v.state.SwarmEntry(req)
	if err == nil && !v.root.IsZero() && signer.IsEqual(v.root) {
		return true
	}
	m, ok := v.Membership(signer)
	return err == nil && ok && user.MayAnswer(m.Tier, obj)
}
func (v *fakeSwarmView) Membership(id *astral.Identity) (*user.Membership, bool) {
	tier, ok := v.tiers[id.String()]
	if !ok {
		return nil, false
	}
	return &user.Membership{Tier: tier}, true
}

type answerFixture struct {
	ops     *SwarmAnswerOps
	state   *answerSwarmState
	view    *fakeSwarmView
	auth    *recordingAuth
	nodeKey *crypto.PrivateKey
	userKey *crypto.PrivateKey
	caller  *astral.Identity
	origin  string // the query's origin; empty is unset
}

// user is the identity of the fixture's user key.
func (f *answerFixture) user() *astral.Identity {
	return secp256k1.Identity(secp256k1.PublicKey(f.userKey))
}

func newAnswerFixture(tier user.Tier) *answerFixture {
	nodeKey, userKey := secp256k1.New(), secp256k1.New()
	node := secp256k1.Identity(secp256k1.PublicKey(nodeKey))
	state := newAnswerSwarmState()
	f := &answerFixture{
		state:   state,
		view:    &fakeSwarmView{status: map[string]user.RequestStatus{}, tiers: map[string]user.Tier{}, state: state},
		auth:    &recordingAuth{verdict: true},
		nodeKey: nodeKey,
		userKey: userKey,
		caller:  astral.GenerateIdentity(),
	}
	f.view.root = f.user()
	if tier != "" {
		f.view.tiers[node.String()] = tier
	}
	writer := &swarmWriter{self: node, signer: heldKeySigner{key: nodeKey}, view: f.view, entry: state.SwarmEntry, merge: state.merge}
	ceremony := newUserKeyCeremony(f.user(), heldKeySigner{key: userKey}, nil, nil)
	f.ops = &SwarmAnswerOps{
		Node:    node,
		State:   writeState{swarmReader: state, swarmWriter: writer},
		View:    f.view,
		Auth:    f.auth,
		UserKey: func() *userKeyCeremony { return ceremony },
		AsUser:  func(uk *userKeyCeremony) answerer { return writer.as(uk.user, uk.signer) },
	}
	return f
}

func testSig() *crypto.Signature {
	return &crypto.Signature{Scheme: crypto.SchemeASN1, Data: []byte{1}}
}

func (f *answerFixture) contractRequest(t *testing.T, filedAt time.Time, status user.RequestStatus) *astral.ObjectID {
	t.Helper()
	body := user.NewContractRequest(astral.GenerateIdentity(), astral.GenerateIdentity(), []*auth.Permit{{Action: "mod.services.discover_action"}})
	body.FiledAt = astral.Time(filedAt)
	body.Timeout = astral.Time(filedAt.Add(time.Hour))
	id := f.state.store(t, &user.SignedContractRequest{ContractRequest: body, FiledBySig: testSig()})
	f.view.status[id.String()] = status
	return id
}

func (f *answerFixture) expelRequest(t *testing.T, status user.RequestStatus) *astral.ObjectID {
	t.Helper()
	body := user.NewExpelRequest(astral.GenerateIdentity(), astral.GenerateIdentity())
	id := f.state.store(t, &user.SignedExpelRequest{ExpelRequest: body, FiledBySig: testSig()})
	f.view.status[id.String()] = status
	return id
}

func (f *answerFixture) joinRequest(t *testing.T, tier user.Tier) *astral.ObjectID {
	t.Helper()
	body := user.NewJoinRequest(astral.GenerateIdentity(), astral.GenerateIdentity(), tier)
	return f.state.store(t, &user.SignedJoinRequest{JoinRequest: body, JoinerSig: testSig()})
}

// run routes one query to fn and returns every object the op sent.
func (f *answerFixture) run(t *testing.T, fn any, queryString string) []astral.Object {
	t.Helper()
	w := newRecordingWriter()
	if err := routeFrom(t, fn, f.caller, f.origin, queryString, w); err != nil {
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
	objs := f.run(t, f.ops.OpAnswerRequest, q)
	if len(objs) != 1 {
		t.Fatalf("answer_request sent %d objects; want 1", len(objs))
	}
	return objs[0]
}

// Done: list shows the open requests, pending or offered, with their status.
func TestSwarmRequestsListsPendingWithStatus(t *testing.T) {
	f := newAnswerFixture(user.TierPrivilegedMember)
	base := time.Now().Add(-30 * time.Minute)
	later := f.contractRequest(t, base.Add(2*time.Minute), user.RequestPending)
	earlier := f.contractRequest(t, base, user.RequestPending)
	offered := f.contractRequest(t, base.Add(time.Minute), user.RequestOffered)
	fulfilled := f.contractRequest(t, base.Add(3*time.Minute), user.RequestFulfilled)
	expired := f.contractRequest(t, base.Add(-2*time.Hour), user.RequestExpired)

	objs := f.run(t, f.ops.OpSwarmRequests, "user.swarm_requests")
	assertListed(t, objs, []*astral.ObjectID{earlier, offered, later},
		[]user.RequestStatus{user.RequestPending, user.RequestOffered, user.RequestPending})

	objs = f.run(t, f.ops.OpSwarmRequests, "user.swarm_requests?all=true")
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
	f := newAnswerFixture(user.TierPrivilegedMember)
	req := f.contractRequest(t, time.Now(), user.RequestPending)
	obj, _ := f.state.SwarmEntry(req)
	cr := obj.(*user.SignedContractRequest)

	sc, ok := f.answer(t, req, "approve").(*auth.SignedContract)
	if !ok {
		t.Fatal("answer_request did not send an offer")
	}
	switch {
	case !sc.Issuer.IsEqual(f.ops.Node), !sc.Subject.IsEqual(cr.FiledBy), !sc.RequestID.IsEqual(req):
		t.Fatalf("offer from %v to %v for %v; want from this node to the filer for the request", sc.Issuer, sc.Subject, sc.RequestID)
	case sc.SubjectSig != nil:
		t.Fatal("the offer carries a subject signature")
	case !cr.CoversOffer(sc.Contract) || len(sc.Permits) != len(cr.Permits):
		t.Fatalf("offer permits %v; want every requested permit one hop deeper", sc.Permits)
	case sc.ExpiresAt.Time().After(time.Now().Add(usermod.OfferValidity)):
		t.Fatalf("offer expires %v; want within 3650 days", sc.ExpiresAt)
	}
	f.assertSignedAndWritten(t, sc, sc.SignableHash(), sc.IssuerSig)
}

// The offer expires with the issuer's membership when that ends first.
func TestAnswerRequestOfferEndsWithTheMembership(t *testing.T) {
	f := newAnswerFixture("")
	ends := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	f.ops.View = &membershipView{fakeSwarmView: f.view, until: ends}
	f.ops.State.(writeState).swarmWriter.view = f.ops.View.(writerView)
	req := f.contractRequest(t, time.Now(), user.RequestPending)

	sc, ok := f.answer(t, req, "approve").(*auth.SignedContract)
	if !ok || !sc.ExpiresAt.Time().Equal(ends) {
		t.Fatalf("answered %v; want an offer expiring with the membership at %v", sc, ends)
	}
}

// membershipView is a fakeSwarmView whose every membership is Privileged
// until until.
type membershipView struct {
	*fakeSwarmView
	until time.Time
}

func (v *membershipView) Membership(id *astral.Identity) (*user.Membership, bool) {
	c := &auth.Contract{Issuer: v.root, Subject: id, ExpiresAt: astral.Time(v.until)}
	return &user.Membership{SignedContract: &auth.SignedContract{Contract: c}, Tier: user.TierPrivilegedMember}, true
}

func (v *membershipView) MayAnswer(signer *astral.Identity, req *astral.ObjectID) bool {
	obj, err := v.state.SwarmEntry(req)
	return err == nil && user.MayAnswer(user.TierPrivilegedMember, obj)
}

// Done: approving an expel request writes an Approval signed by this node.
func TestAnswerRequestApprovesAnExpelRequest(t *testing.T) {
	f := newAnswerFixture(user.TierPrivilegedMember)
	req := f.expelRequest(t, user.RequestPending)

	sa, ok := f.answer(t, req, "approve").(*user.SignedApproval)
	if !ok {
		t.Fatal("answer_request did not send a signed approval")
	}
	if !sa.Issuer.IsEqual(f.ops.Node) || !sa.RequestID.IsEqual(req) {
		t.Fatalf("approval names issuer %v and request %v", sa.Issuer, sa.RequestID)
	}
	f.assertSignedAndWritten(t, sa, sa.SignableHash(), sa.IssuerSig)
}

// Done: answer writes a Decline signed by this node, also against an approved
// expel request and an offered contract request.
func TestAnswerRequestWritesSignedDecline(t *testing.T) {
	for name, req := range map[string]func(f *answerFixture) *astral.ObjectID{
		"approved expel request":   func(f *answerFixture) *astral.ObjectID { return f.expelRequest(t, user.RequestApproved) },
		"offered contract request": func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestOffered) },
	} {
		f := newAnswerFixture(user.TierPrivilegedMember)
		id := req(f)
		sd, ok := f.answer(t, id, "decline").(*user.SignedDecline)
		if !ok {
			t.Fatalf("%s: answer_request did not send a signed decline", name)
		}
		if !sd.Issuer.IsEqual(f.ops.Node) || !sd.RequestID.IsEqual(id) {
			t.Fatalf("%s: decline names issuer %v and request %v", name, sd.Issuer, sd.RequestID)
		}
		f.assertSignedAndWritten(t, sd, sd.SignableHash(), sd.IssuerSig)
	}
}

// A held answer (user.ErrSwarmEntryHeld) may still be stored, so it is sent;
// any other write failure is sent as an error.
func TestAnswerRequestSendsHeldAnswer(t *testing.T) {
	f := newAnswerFixture(user.TierPrivilegedMember)
	req := f.contractRequest(t, time.Now(), user.RequestPending)
	f.state.addErr = fmt.Errorf("add: %w", usermod.ErrSwarmEntryHeld)
	if _, ok := f.answer(t, req, "approve").(*auth.SignedContract); !ok {
		t.Fatal("a held offer was not sent")
	}

	f = newAnswerFixture(user.TierPrivilegedMember)
	req = f.contractRequest(t, time.Now(), user.RequestPending)
	f.state.addErr = fmt.Errorf("add: %w", usermod.ErrSwarmEntryDropped)
	if obj := f.answer(t, req, "approve"); obj.ObjectType() == (&auth.SignedContract{}).ObjectType() {
		t.Fatal("a dropped offer was sent as written")
	}
}

func (f *answerFixture) assertSignedAndWritten(t *testing.T, obj astral.Object, hash []byte, sig *crypto.Signature) {
	t.Helper()
	f.assertSignedAndWrittenBy(t, f.nodeKey, obj, hash, sig)
}

func (f *answerFixture) assertSignedAndWrittenBy(t *testing.T, key *crypto.PrivateKey, obj astral.Object, hash []byte, sig *crypto.Signature) {
	t.Helper()
	if err := secp256k1sig.VerifyASN1(secp256k1.PublicKey(key), hash, sig); err != nil {
		t.Fatalf("answer is not signed by the expected key: %v", err)
	}
	written := f.state.written()
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
		{"member declines a join", user.TierMember, func(f *answerFixture) *astral.ObjectID { return f.joinRequest(t, user.TierMember) }, "decline", ErrOutsideTier},
		{"member declines a contract request", user.TierMember, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestPending) }, "decline", ErrOutsideTier},
		{"trusted approves a contract request", user.TierTrusted, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestPending) }, "approve", ErrOutsideTier},
		{"trusted declines a trusted join", user.TierTrusted, func(f *answerFixture) *astral.ObjectID { return f.joinRequest(t, user.TierTrusted) }, "decline", ErrOutsideTier},
		{"no membership", "", func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestPending) }, "approve", ErrOutsideTier},
		{"approve an approved expel request", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID { return f.expelRequest(t, user.RequestApproved) }, "approve", ErrRequestClosed},
		{"approve an offered expel request", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID { return f.expelRequest(t, user.RequestOffered) }, "approve", ErrRequestClosed},
		{"approve a fulfilled contract request", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID {
			return f.contractRequest(t, time.Now(), user.RequestFulfilled)
		}, "approve", ErrRequestClosed},
		{"decline a fulfilled contract request", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID {
			return f.contractRequest(t, time.Now(), user.RequestFulfilled)
		}, "decline", ErrRequestClosed},
		{"decline an expired request", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestExpired) }, "decline", ErrRequestClosed},
		{"approve a declined request", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestDeclined) }, "approve", ErrRequestClosed},
		{"approve a join", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID { return f.joinRequest(t, user.TierMember) }, "approve", ErrRequestClosed},
		{"unknown request", user.TierPrivilegedMember, func(*answerFixture) *astral.ObjectID { return testObjectID() }, "approve", ErrUnknownRequest},
		{"invalid answer", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestPending) }, "maybe", ErrInvalidAnswer},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newAnswerFixture(c.tier)
			got := f.answer(t, c.req(f), c.answer)
			if e, ok := got.(astral.Error); !ok || e.Error() != c.want.Error() {
				t.Fatalf("answered %v; want %v", got, c.want)
			}
			if n := len(f.state.written()); n != 0 {
				t.Fatalf("wrote %d entries; want none", n)
			}
		})
	}
}

// An issuer offers at most once per request: a second approve by this node is
// refused, while another issuer may still offer for the offered request.
func TestAnswerRequestOffersOncePerIssuer(t *testing.T) {
	f := newAnswerFixture(user.TierPrivilegedMember)
	req := f.contractRequest(t, time.Now(), user.RequestPending)
	if _, ok := f.answer(t, req, "approve").(*auth.SignedContract); !ok {
		t.Fatal("the first offer was not sent")
	}
	f.view.status[req.String()] = user.RequestOffered
	if got := f.answer(t, req, "approve"); got.(astral.Error).Error() != ErrRequestClosed.Error() {
		t.Fatalf("a second offer from this node: %v; want %v", got, ErrRequestClosed)
	}

	f.caller = f.user()
	sc, ok := f.answerAs(t, req, "approve", "user").(*auth.SignedContract)
	if !ok || !sc.Issuer.IsEqual(f.user()) {
		t.Fatalf("the user key's offer after this node's: %v; want one from the user key", sc)
	}
}

// A Trusted node declines a Member-tier JoinRequest (D5), and approves none:
// its carrier signs the membership.
//
// note: ported from TestAnswerRequestTrustedApprovesMemberJoin; a join has
// no Approval since the two-signature model.
func TestAnswerRequestTrustedDeclinesMemberJoin(t *testing.T) {
	f := newAnswerFixture(user.TierTrusted)
	req := f.joinRequest(t, user.TierMember)
	if _, ok := f.answer(t, req, "decline").(*user.SignedDecline); !ok {
		t.Fatal("a trusted node did not decline a member join")
	}
	if got := f.answer(t, req, "approve"); got.(astral.Error).Error() != ErrRequestClosed.Error() {
		t.Fatalf("a trusted node approving a join: %v; want %v", got, ErrRequestClosed)
	}
}

// With as=user the user key signs the answer: only for a local query by the
// user, on a node that reaches the user key. Its offer is not capped by a
// membership.
func TestAnswerRequestAsTheUser(t *testing.T) {
	f := newAnswerFixture("")
	req := f.contractRequest(t, time.Now(), user.RequestPending)
	f.caller = f.user()
	sc, ok := f.answerAs(t, req, "approve", "user").(*auth.SignedContract)
	if !ok || !sc.Issuer.IsEqual(f.user()) || sc.SubjectSig != nil {
		t.Fatalf("answered %v; want an offer the user key issued", sc)
	}
	if d := time.Until(sc.ExpiresAt.Time()); d < usermod.OfferValidity-time.Minute {
		t.Fatalf("the user key's offer expires in %v; want 3650 days", d)
	}
	f.assertSignedAndWrittenBy(t, f.userKey, sc, sc.SignableHash(), sc.IssuerSig)

	for _, c := range []struct {
		name   string
		as     string
		origin string
		caller func(f *answerFixture) *astral.Identity
		noKey  bool
		want   error
	}{
		{"an unknown as", "owner", "", (*answerFixture).user, false, ErrInvalidAnswer},
		{"over the network", "user", astral.OriginNetwork, (*answerFixture).user, false, astral.NewErrOriginNotAllowed(astral.OriginNetwork)},
		{"without the user key", "user", "", (*answerFixture).user, true, ErrNoUserKey},
		{"by another caller", "user", "", func(*answerFixture) *astral.Identity { return astral.GenerateIdentity() }, false, auth.ErrNotPermitted},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newAnswerFixture(user.TierPrivilegedMember)
			req := f.contractRequest(t, time.Now(), user.RequestPending)
			f.caller, f.origin = c.caller(f), c.origin
			if c.noKey {
				f.ops.UserKey = func() *userKeyCeremony { return nil }
			}
			got := f.answerAs(t, req, "approve", c.as)
			if e, ok := got.(astral.Error); !ok || e.Error() != c.want.Error() {
				t.Fatalf("answered %v; want %v", got, c.want)
			}
			if n := len(f.state.written()); n != 0 {
				t.Fatalf("wrote %d entries; want none", n)
			}
		})
	}
}

// The caller needs AdminSwarmAction naming the request's subject to answer,
// and SeeSwarmAction to list.
func TestSwarmAnswerOpsAuthorizeCaller(t *testing.T) {
	f := newAnswerFixture(user.TierPrivilegedMember)
	req := f.contractRequest(t, time.Now(), user.RequestPending)
	f.auth.verdict = false

	if got := f.answer(t, req, "approve"); got.(astral.Error).Error() != auth.ErrNotPermitted.Error() {
		t.Fatalf("answered %v; want not permitted", got)
	}
	objs := f.run(t, f.ops.OpSwarmRequests, "user.swarm_requests")
	if len(objs) != 1 || objs[0].(astral.Error).Error() != auth.ErrNotPermitted.Error() {
		t.Fatalf("listed %v; want not permitted", objs)
	}
	if len(f.state.written()) != 0 {
		t.Fatal("a refused caller wrote an entry")
	}

	actions := f.auth.recorded()
	admin, ok := actions[0].(*user.AdminSwarmAction)
	stored, _ := f.state.SwarmEntry(req)
	if !ok || !admin.Subject.IsEqual(stored.(*user.SignedContractRequest).Identity) {
		t.Fatalf("answer asked %v; want an admin swarm action on the request's identity", actions[0])
	}
	if _, ok := actions[1].(*user.SeeSwarmAction); !ok {
		t.Fatalf("list asked %v; want a see swarm action", actions[1])
	}
}
