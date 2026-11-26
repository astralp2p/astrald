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

// fakeSwarmView answers statuses and tiers from maps, and MayAnswer by the
// tier over the requests state stores; every other method returns nothing.
type fakeSwarmView struct {
	status map[string]user.RequestStatus
	tiers  map[string]user.Tier
	state  *answerSwarmState
}

func (v *fakeSwarmView) RequestStatus(req *astral.ObjectID) user.RequestStatus {
	if s, ok := v.status[req.String()]; ok {
		return s
	}
	return user.RequestPending
}
func (v *fakeSwarmView) Expelled(*astral.Identity) bool                            { return false }
func (v *fakeSwarmView) ApprovalsTouching(*astral.Identity) []*user.SignedApproval { return nil }
func (v *fakeSwarmView) CarriedOut(*astral.ObjectID) []*auth.SignedContract        { return nil }
func (v *fakeSwarmView) MayAnswer(signer *astral.Identity, req *astral.ObjectID) bool {
	obj, err := v.state.SwarmEntry(req)
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
	caller  *astral.Identity
	origin  string // the query's origin; empty is unset
}

func newAnswerFixture(tier user.Tier) *answerFixture {
	nodeKey := secp256k1.New()
	node := secp256k1.Identity(secp256k1.PublicKey(nodeKey))
	state := newAnswerSwarmState()
	f := &answerFixture{
		state:   state,
		view:    &fakeSwarmView{status: map[string]user.RequestStatus{}, tiers: map[string]user.Tier{}, state: state},
		auth:    &recordingAuth{verdict: true},
		nodeKey: nodeKey,
		caller:  astral.GenerateIdentity(),
	}
	if tier != "" {
		f.view.tiers[node.String()] = tier
	}
	writer := &swarmWriter{self: node, signer: heldKeySigner{key: nodeKey}, view: f.view, merge: state.merge}
	f.ops = &SwarmAnswerOps{
		Node:  node,
		State: writeState{swarmReader: state, swarmWriter: writer},
		View:  f.view,
		Auth:  f.auth,
	}
	return f
}

func testSig() *crypto.Signature {
	return &crypto.Signature{Scheme: crypto.SchemeASN1, Data: []byte{1}}
}

func (f *answerFixture) contractRequest(t *testing.T, filedAt time.Time, status user.RequestStatus) *astral.ObjectID {
	t.Helper()
	body := user.NewContractRequest(astral.GenerateIdentity(), astral.GenerateIdentity(), nil)
	body.FiledAt = astral.Time(filedAt)
	body.Timeout = astral.Time(filedAt.Add(time.Hour))
	id := f.state.store(t, &user.SignedContractRequest{ContractRequest: body, FiledBySig: testSig()})
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
	objs := f.run(t, f.ops.OpAnswerRequest, "user.answer_request?id="+id.String()+"&answer="+answer)
	if len(objs) != 1 {
		t.Fatalf("answer_request sent %d objects; want 1", len(objs))
	}
	return objs[0]
}

// Done: list shows pending requests with their status.
func TestSwarmRequestsListsPendingWithStatus(t *testing.T) {
	f := newAnswerFixture(user.TierPrivilegedMember)
	base := time.Now().Add(-30 * time.Minute)
	later := f.contractRequest(t, base.Add(2*time.Minute), user.RequestPending)
	earlier := f.contractRequest(t, base, user.RequestPending)
	approved := f.contractRequest(t, base.Add(time.Minute), user.RequestApproved)
	expired := f.contractRequest(t, base.Add(-2*time.Hour), user.RequestExpired)

	objs := f.run(t, f.ops.OpSwarmRequests, "user.swarm_requests")
	assertListed(t, objs, []*astral.ObjectID{earlier, later},
		[]user.RequestStatus{user.RequestPending, user.RequestPending})

	objs = f.run(t, f.ops.OpSwarmRequests, "user.swarm_requests?all=true")
	assertListed(t, objs, []*astral.ObjectID{expired, earlier, approved, later},
		[]user.RequestStatus{user.RequestExpired, user.RequestPending, user.RequestApproved, user.RequestPending})
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

// Done: answer writes an Approval signed by this node.
func TestAnswerRequestWritesSignedApproval(t *testing.T) {
	f := newAnswerFixture(user.TierPrivilegedMember)
	req := f.contractRequest(t, time.Now(), user.RequestPending)

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
// request.
func TestAnswerRequestWritesSignedDecline(t *testing.T) {
	f := newAnswerFixture(user.TierPrivilegedMember)
	req := f.contractRequest(t, time.Now(), user.RequestApproved)

	sd, ok := f.answer(t, req, "decline").(*user.SignedDecline)
	if !ok {
		t.Fatal("answer_request did not send a signed decline")
	}
	if !sd.Issuer.IsEqual(f.ops.Node) || !sd.RequestID.IsEqual(req) {
		t.Fatalf("decline names issuer %v and request %v", sd.Issuer, sd.RequestID)
	}
	f.assertSignedAndWritten(t, sd, sd.SignableHash(), sd.IssuerSig)
}

// A held answer (user.ErrSwarmEntryHeld) may still be stored, so it is sent;
// any other write failure is sent as an error.
func TestAnswerRequestSendsHeldAnswer(t *testing.T) {
	f := newAnswerFixture(user.TierPrivilegedMember)
	req := f.contractRequest(t, time.Now(), user.RequestPending)
	f.state.addErr = fmt.Errorf("add: %w", usermod.ErrSwarmEntryHeld)
	if _, ok := f.answer(t, req, "approve").(*user.SignedApproval); !ok {
		t.Fatal("a held approval was not sent")
	}

	f.state.addErr = fmt.Errorf("add: %w", usermod.ErrSwarmEntryDropped)
	if obj := f.answer(t, req, "approve"); obj.ObjectType() == (&user.SignedApproval{}).ObjectType() {
		t.Fatal("a dropped approval was sent as written")
	}
}

func (f *answerFixture) assertSignedAndWritten(t *testing.T, obj astral.Object, hash []byte, sig *crypto.Signature) {
	t.Helper()
	if err := secp256k1sig.VerifyASN1(secp256k1.PublicKey(f.nodeKey), hash, sig); err != nil {
		t.Fatalf("answer is not signed by this node: %v", err)
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
		{"member approves a join", user.TierMember, func(f *answerFixture) *astral.ObjectID { return f.joinRequest(t, user.TierMember) }, "approve", ErrOutsideTier},
		{"member declines a contract request", user.TierMember, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestPending) }, "decline", ErrOutsideTier},
		{"trusted approves a contract request", user.TierTrusted, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestPending) }, "approve", ErrOutsideTier},
		{"trusted approves a trusted join", user.TierTrusted, func(f *answerFixture) *astral.ObjectID { return f.joinRequest(t, user.TierTrusted) }, "approve", ErrOutsideTier},
		{"no membership", "", func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestPending) }, "approve", ErrOutsideTier},
		{"approve an approved request", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestApproved) }, "approve", ErrRequestClosed},
		{"decline an expired request", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestExpired) }, "decline", ErrRequestClosed},
		{"approve a declined request", user.TierPrivilegedMember, func(f *answerFixture) *astral.ObjectID { return f.contractRequest(t, time.Now(), user.RequestDeclined) }, "approve", ErrRequestClosed},
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

// A Trusted node answers a Member-tier JoinRequest (D5).
func TestAnswerRequestTrustedApprovesMemberJoin(t *testing.T) {
	f := newAnswerFixture(user.TierTrusted)
	req := f.joinRequest(t, user.TierMember)
	if _, ok := f.answer(t, req, "approve").(*user.SignedApproval); !ok {
		t.Fatal("a trusted node did not approve a member join")
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
