package apphost

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// fakeSwarmState is an in-memory user.SwarmState shared by every node of a
// test swarm: what one node adds, every node reads.
type fakeSwarmState struct {
	mu      sync.Mutex
	entries map[string]astral.Object
	ids     map[user.EntryKind][]*astral.ObjectID
	hold    bool // add holds every entry: it answers ErrSwarmEntryHeld and stores nothing
}

func newFakeSwarmState() *fakeSwarmState {
	return &fakeSwarmState{entries: map[string]astral.Object{}, ids: map[user.EntryKind][]*astral.ObjectID{}}
}

// add merges obj as SwarmState's one merge does.
func (s *fakeSwarmState) add(obj astral.Object) error {
	kind, ok := user.EntryKindOf(obj)
	if !ok {
		return fmt.Errorf("%w: not an entry", usermod.ErrSwarmEntryDropped)
	}
	id, err := astral.ResolveObjectID(obj)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hold {
		return usermod.ErrSwarmEntryHeld
	}
	if _, dup := s.entries[id.String()]; dup {
		return nil
	}
	s.entries[id.String()] = obj
	s.ids[kind] = append(s.ids[kind], id)
	return nil
}

func (s *fakeSwarmState) SwarmEntry(id *astral.ObjectID) (astral.Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.entries[id.String()]
	if !ok {
		return nil, errors.New("not stored")
	}
	return obj, nil
}

func (s *fakeSwarmState) SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*astral.ObjectID(nil), s.ids[kind]...), nil
}

func (s *fakeSwarmState) SwarmHead() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return uint64(len(s.entries))
}

// hostState is one host's SwarmState over the shared fakeSwarmState: its
// typed writes sign as that host, and a held entry counts as written.
type hostState struct {
	*fakeSwarmState
	node *astral.Identity
	now  func() time.Time
}

var _ hostSwarmState = &hostState{}

func (h *hostState) write(obj astral.Object) error {
	if err := h.add(obj); err != nil && !errors.Is(err, usermod.ErrSwarmEntryHeld) {
		return err
	}
	return nil
}

func (h *hostState) FileContractRequest(ctx *astral.Context, app *astral.Identity, permits []*auth.Permit) (*user.SignedContractRequest, error) {
	filedAt := astral.Time(h.now())
	req := &user.ContractRequest{FiledBy: h.node, Identity: app, Permits: permits, FiledAt: filedAt, Timeout: user.RequestDeadline(filedAt)}
	sig, err := fakeSigner{}.Sign(ctx, nil, req)
	if err != nil {
		return nil, err
	}
	signed := &user.SignedContractRequest{ContractRequest: req, FiledBySig: sig}
	return signed, h.write(signed)
}

func (h *hostState) AddContract(_ *astral.Context, sc *auth.SignedContract) error { return h.write(sc) }

// objects returns the stored entries of kind.
func (s *fakeSwarmState) objects(kind user.EntryKind) []astral.Object {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []astral.Object
	for _, id := range s.ids[kind] {
		list = append(list, s.entries[id.String()])
	}
	return list
}

// fakeSwarmView is a user.SwarmView over a fakeSwarmState: declined beats
// approved beats expired beats pending. ApprovalsTouching returns every
// stored Approval, wider than the real view, so the host worker must pick its
// own requests itself.
type fakeSwarmView struct {
	state      *fakeSwarmState
	now        func() time.Time
	membership *user.Membership
}

var _ hostSwarmView = &fakeSwarmView{}

func (v *fakeSwarmView) RequestStatus(req *astral.ObjectID) user.RequestStatus {
	for _, obj := range v.state.objects(user.EntryDeclines) {
		if obj.(*user.SignedDecline).RequestID.IsEqual(req) {
			return user.RequestDeclined
		}
	}
	for _, obj := range v.state.objects(user.EntryApprovals) {
		if obj.(*user.SignedApproval).RequestID.IsEqual(req) {
			return user.RequestApproved
		}
	}
	if obj, err := v.state.SwarmEntry(req); err == nil {
		if r, ok := obj.(*user.SignedContractRequest); ok && r.Expired(v.now()) {
			return user.RequestExpired
		}
	}
	return user.RequestPending
}

func (v *fakeSwarmView) ApprovalsTouching(*astral.Identity) (list []*user.SignedApproval) {
	for _, obj := range v.state.objects(user.EntryApprovals) {
		list = append(list, obj.(*user.SignedApproval))
	}
	return list
}

func (v *fakeSwarmView) CarriedOut(approval *astral.ObjectID) (list []*auth.SignedContract) {
	for _, obj := range v.state.objects(user.EntryContracts) {
		if c := obj.(*auth.SignedContract); c.ApprovalID.IsEqual(approval) {
			list = append(list, c)
		}
	}
	return list
}

func (v *fakeSwarmView) Membership(*astral.Identity) (*user.Membership, bool) {
	return v.membership, v.membership != nil
}

// fakeSigner signs requests and contracts with a marker naming the signer.
type fakeSigner struct{}

func (fakeSigner) Sign(_ *astral.Context, _ *crypto.PublicKey, obj crypto.SignableTextObject) (*crypto.Signature, error) {
	return &crypto.Signature{Scheme: "test", Data: astral.Bytes16(obj.SignableHash())}, nil
}

func (fakeSigner) SignContract(_ *astral.Context, c *auth.SignedContract) error {
	c.IssuerSig = &crypto.Signature{Scheme: "test", Data: astral.Bytes16(c.Issuer.String())}
	c.SubjectSig = &crypto.Signature{Scheme: "test", Data: astral.Bytes16(c.Subject.String())}
	return nil
}

// testSwarm is a shared SwarmState, its view and a clock every node reads.
type testSwarm struct {
	state *fakeSwarmState
	view  *fakeSwarmView
	mu    sync.Mutex
	clock time.Time
}

func newTestSwarm() *testSwarm {
	s := &testSwarm{state: newFakeSwarmState(), clock: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)}
	s.view = &fakeSwarmView{state: s.state, now: s.now}
	return s
}

func (s *testSwarm) now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clock
}

func (s *testSwarm) advance(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock = s.clock.Add(d)
}

// host returns the contract requests of a new node in s.
func (s *testSwarm) host() *contractRequests {
	node := astral.GenerateIdentity()
	cr := newContractRequests(node, &hostState{fakeSwarmState: s.state, node: node, now: s.now}, s.view, fakeSigner{})
	cr.now = s.now
	return cr
}

// answer adds a Privileged node's Approval (approve) or Decline of req and
// returns the Approval's entry ID, or nil for a Decline.
func (s *testSwarm) answer(t *testing.T, req *astral.ObjectID, approve bool) *astral.ObjectID {
	t.Helper()
	issuer := astral.GenerateIdentity()
	sig := &crypto.Signature{Scheme: "test", Data: astral.Bytes16(issuer.String())}
	var entry astral.Object = &user.SignedDecline{Decline: user.NewDecline(issuer, req), IssuerSig: sig}
	if approve {
		entry = &user.SignedApproval{Approval: user.NewApproval(issuer, req), IssuerSig: sig}
	}
	if err := s.state.add(entry); err != nil {
		t.Fatalf("add answer: %v", err)
	}
	if !approve {
		return nil
	}
	id, err := astral.ResolveObjectID(entry)
	if err != nil {
		t.Fatalf("approval id: %v", err)
	}
	return id
}

// contracts returns the contracts stored in s.
func (s *testSwarm) contracts() (list []*auth.SignedContract) {
	for _, obj := range s.state.objects(user.EntryContracts) {
		list = append(list, obj.(*auth.SignedContract))
	}
	return list
}

func mustFile(t *testing.T, cr *contractRequests, app *astral.Identity, p []*auth.Permit) *astral.ObjectID {
	t.Helper()
	id, err := cr.File(astral.NewContext(nil), app, p)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	return id
}

func mustPoll(t *testing.T, cr *contractRequests) {
	t.Helper()
	if err := cr.Poll(astral.NewContext(nil)); err != nil {
		t.Fatalf("poll: %v", err)
	}
}

func walletPermits() []*auth.Permit { return servicePermits("bitcoin-wallet") }

// servicePermits returns one permit to discover the named service.
func servicePermits(name string) []*auth.Permit {
	return []*auth.Permit{discoveryPermit(astral.String8(name))}
}

// Done: a request is filed once. A second call, a call with the same permits
// in another order, and a call after the host restarts all return the one
// stored request; only a changed permit set files another.
func TestContractRequestIsFiledOnce(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	app := astral.GenerateIdentity()
	asked := permits("mod.auth.see_objects_action", "mod.auth.store_objects_action")

	first := mustFile(t, host, app, asked)
	again := mustFile(t, host, app, asked)
	reordered := mustFile(t, host, app, []*auth.Permit{asked[1], asked[0]})

	restarted := swarm.host()
	restarted.node = host.node
	afterRestart := mustFile(t, restarted, app, asked)

	for name, id := range map[string]*astral.ObjectID{"again": again, "reordered": reordered, "after a restart": afterRestart} {
		if !id.IsEqual(first) {
			t.Errorf("%v: filed %v; want the first request %v", name, id, first)
		}
	}
	reqs := swarm.state.objects(user.EntryRequests)
	if len(reqs) != 1 {
		t.Fatalf("SwarmState holds %v requests; want 1", len(reqs))
	}
	r := reqs[0].(*user.SignedContractRequest)
	switch {
	case !r.FiledBy.IsEqual(host.node), !r.Identity.IsEqual(app):
		t.Errorf("request filed by %v for %v; want by the host for the app", r.FiledBy, r.Identity)
	case r.Timeout.Time().Sub(r.FiledAt.Time()) != user.RequestTimeout:
		t.Errorf("timeout %v after filing; want %v", r.Timeout.Time().Sub(r.FiledAt.Time()), user.RequestTimeout)
	case r.FiledBySig == nil:
		t.Error("request is not signed")
	}

	if other := mustFile(t, host, app, asked[:1]); other.IsEqual(first) {
		t.Error("a changed permit set returned the first request")
	}
}

// A request SwarmState holds (ErrSwarmEntryHeld) is filed, not filed again.
func TestHeldContractRequestIsFiledOnce(t *testing.T) {
	swarm := newTestSwarm()
	swarm.state.hold = true
	host := swarm.host()
	app := astral.GenerateIdentity()

	first := mustFile(t, host, app, walletPermits())
	if again := mustFile(t, host, app, walletPermits()); !again.IsEqual(first) {
		t.Fatalf("filed %v after a held %v", again, first)
	}
	if c, err := host.Outcome(first); c != nil || err != nil {
		t.Fatalf("a held request has outcome %v, %v; want pending", c, err)
	}
}

// Done: a contract is signed only for the identity the approved request
// names, carrying its ApprovalID, and only once.
func TestApprovedRequestSignsContractForNamedIdentity(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	app := astral.GenerateIdentity()
	reqID := mustFile(t, host, app, walletPermits())

	mustPoll(t, host)
	if n := len(swarm.contracts()); n != 0 {
		t.Fatalf("signed %v contracts before an Approval", n)
	}

	approvalID := swarm.answer(t, reqID, true)
	mustPoll(t, host)
	mustPoll(t, host)

	contracts := swarm.contracts()
	if len(contracts) != 1 {
		t.Fatalf("SwarmState holds %v contracts; want 1", len(contracts))
	}
	c := contracts[0]
	switch {
	case !c.Issuer.IsEqual(host.node):
		t.Errorf("issuer %v; want the host", c.Issuer)
	case !c.Subject.IsEqual(app):
		t.Errorf("subject %v; want the app the request names", c.Subject)
	case !c.ApprovalID.IsEqual(approvalID):
		t.Errorf("ApprovalID %v; want %v", c.ApprovalID, approvalID)
	case !samePermits(&user.ContractRequest{Permits: c.Permits}, &user.ContractRequest{Permits: walletPermits()}):
		t.Errorf("permits %v; want the requested ones", c.Permits)
	case c.IssuerSig == nil:
		t.Error("contract is not signed")
	}

	got, err := host.Outcome(reqID)
	if err != nil || got != c {
		t.Fatalf("outcome %v, %v; want the signed contract", got, err)
	}
}

// The contract never outlives the host's membership.
func TestAppContractExpiresWithHostMembership(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	ends := swarm.now().Add(48 * time.Hour)
	swarm.view.membership = &user.Membership{SignedContract: &auth.SignedContract{Contract: &auth.Contract{
		Issuer: astral.GenerateIdentity(), Subject: host.node, ExpiresAt: astral.Time(ends),
	}}, Tier: user.TierMember}

	reqID := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
	swarm.answer(t, reqID, true)
	mustPoll(t, host)

	if c := swarm.contracts(); len(c) != 1 || !c[0].ExpiresAt.Time().Equal(ends) {
		t.Fatalf("contracts %v; want one expiring with the membership at %v", c, ends)
	}
}

// Done: another app cannot reuse that Approval. App B's request stays
// pending, B gets no contract, and A's contract names A alone.
func TestAnotherAppCannotReuseApproval(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	appA, appB := astral.GenerateIdentity(), astral.GenerateIdentity()
	reqA := mustFile(t, host, appA, walletPermits())
	reqB := mustFile(t, host, appB, walletPermits())
	if reqA.IsEqual(reqB) {
		t.Fatal("app B got app A's request")
	}

	approvalA := swarm.answer(t, reqA, true)
	mustPoll(t, host)

	contracts := swarm.contracts()
	if len(contracts) != 1 || !contracts[0].Subject.IsEqual(appA) || !contracts[0].ApprovalID.IsEqual(approvalA) {
		t.Fatalf("contracts %v; want only app A's, carrying A's Approval", contracts)
	}
	if c, err := host.Outcome(reqB); c != nil || err != nil {
		t.Fatalf("app B's outcome %v, %v; want pending", c, err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(50 * time.Millisecond)
	defer cancel()
	if c, err := host.Wait(ctx, reqB); c != nil || !errors.Is(err, ctx.Err()) {
		t.Fatalf("app B's wait got %v, %v; want only the deadline", c, err)
	}
}

// Done: an app moved to another node files a fresh request there. The new
// host does not carry out the old host's Approval, and signs once its own
// request is approved.
func TestMovedAppFilesFreshRequest(t *testing.T) {
	swarm := newTestSwarm()
	oldHost, newHost := swarm.host(), swarm.host()
	app := astral.GenerateIdentity()

	oldReq := mustFile(t, oldHost, app, walletPermits())
	swarm.answer(t, oldReq, true)
	mustPoll(t, oldHost)

	newReq := mustFile(t, newHost, app, walletPermits())
	if newReq.IsEqual(oldReq) {
		t.Fatal("the new host returned the old host's request")
	}
	obj, err := swarm.state.SwarmEntry(newReq)
	if err != nil || !obj.(*user.SignedContractRequest).FiledBy.IsEqual(newHost.node) {
		t.Fatalf("fresh request %v, %v; want one filed by the new host", obj, err)
	}

	mustPoll(t, newHost)
	if n := len(swarm.contracts()); n != 1 {
		t.Fatalf("the new host signed on the old host's Approval: %v contracts", n)
	}

	newApproval := swarm.answer(t, newReq, true)
	mustPoll(t, newHost)
	c, err := newHost.Outcome(newReq)
	if err != nil || c == nil || !c.Issuer.IsEqual(newHost.node) || !c.ApprovalID.IsEqual(newApproval) {
		t.Fatalf("new host outcome %v, %v; want its own contract on its own Approval", c, err)
	}
}

// Rate limiting: each app files at most ContractRequestsPerApp new requests
// per window, the node at most ContractRequestsPerNode; a live request
// returned again does not count, and the window frees up as it passes.
func TestContractRequestsAreRateLimited(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	app := astral.GenerateIdentity()
	ctx := astral.NewContext(nil)

	for i := 0; i < ContractRequestsPerApp; i++ {
		asked := servicePermits(fmt.Sprintf("service-%d", i))
		mustFile(t, host, app, asked)
		mustFile(t, host, app, asked) // the live request again: not counted
	}
	if _, err := host.File(ctx, app, servicePermits("one-too-many")); !errors.Is(err, apphostmod.ErrContractRequestRateLimited) {
		t.Fatalf("request over the app's limit: %v; want rate limited", err)
	}
	mustFile(t, host, astral.GenerateIdentity(), servicePermits("other-app"))

	swarm.advance(ContractRequestWindow)
	mustFile(t, host, app, servicePermits("next-window"))

	limiter := newRequestLimiter(time.Hour, 100, 3)
	now := swarm.now()
	for i := 0; i < 3; i++ {
		if !limiter.allow(astral.GenerateIdentity(), now) {
			t.Fatalf("request %v under the node limit refused", i)
		}
	}
	if limiter.allow(astral.GenerateIdentity(), now) {
		t.Fatal("request over the node limit allowed")
	}
	if n := len(swarm.state.objects(user.EntryRequests)); n != ContractRequestsPerApp+2 {
		t.Fatalf("SwarmState holds %v requests; want %v", n, ContractRequestsPerApp+2)
	}
}

// A Decline, even after the contract, and an expiry reach the waiting app,
// and the app then files a fresh request.
func TestDeclineAndExpiryReachTheApp(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	ctx := astral.NewContext(nil)

	declinedApp := astral.GenerateIdentity()
	declined := mustFile(t, host, declinedApp, walletPermits())
	swarm.answer(t, declined, true)
	mustPoll(t, host)
	swarm.answer(t, declined, false)

	expiring := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
	swarm.advance(user.RequestTimeout + time.Second)

	for id, want := range map[*astral.ObjectID]error{declined: apphostmod.ErrContractDeclined, expiring: apphostmod.ErrContractRequestExpired} {
		wctx, cancel := ctx.WithTimeout(time.Second)
		if _, err := host.Wait(wctx, id); !errors.Is(err, want) {
			t.Errorf("wait: %v; want %v", err, want)
		}
		cancel()
	}

	if again := mustFile(t, host, declinedApp, walletPermits()); again.IsEqual(declined) {
		t.Fatal("a declined request was returned instead of a fresh one")
	}
}

// Wait returns once a poll carries out the Approval.
func TestWaitWakesOnPoll(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	reqID := mustFile(t, host, astral.GenerateIdentity(), walletPermits())

	ctx, cancel := astral.NewContext(nil).WithTimeout(5 * time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		c, err := host.Wait(ctx, reqID)
		if err == nil && c == nil {
			err = errors.New("no contract")
		}
		done <- err
	}()

	swarm.answer(t, reqID, true)
	mustPoll(t, host)
	if err := <-done; err != nil {
		t.Fatalf("wait: %v", err)
	}
}

// apphost.request_contract files for the session's app, sends the request ID,
// then the contract once the Approval is carried out.
func TestRequestContractOp(t *testing.T) {
	swarm := newTestSwarm()
	mod := &Module{log: log.New(nil), contractRequests: swarm.host()}
	app := astral.GenerateIdentity()

	op, err := routing.NewOp(mod.requestContract)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()
	w := newRecordingWriter()
	in, err := op.RouteQuery(ctx, sessionQuery(mod, app, app, "apphost.request_contract"), w)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	sender := channel.NewSender(in)
	for _, obj := range append(objectsOf(walletPermits()), &astral.EOS{}) {
		if err := sender.Send(obj); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	reqID := awaitRequest(t, swarm)
	swarm.answer(t, reqID, true)
	mustPoll(t, mod.contractRequests)
	awaitServeAppsClose(t, w)

	replies := receiveAll(t, w)
	if len(replies) != 2 {
		t.Fatalf("op answered %v objects; want the request ID and the contract", len(replies))
	}
	if id, ok := replies[0].(*astral.ObjectID); !ok || !id.IsEqual(reqID) {
		t.Errorf("first reply %v; want request ID %v", replies[0], reqID)
	}
	if c, ok := replies[1].(*auth.SignedContract); !ok || !c.Subject.IsEqual(app) {
		t.Errorf("second reply %v; want the app's contract", replies[1])
	}
}

// Without contract requests built (until G7), the op answers that they are off.
func TestRequestContractOpOff(t *testing.T) {
	mod := &Module{log: log.New(nil)}
	app := astral.GenerateIdentity()
	w := newRecordingWriter()
	if err := routeQuery(t, mod.requestContract, sessionQuery(mod, app, app, "apphost.request_contract"), w); err != nil {
		t.Fatalf("route: %v", err)
	}
	if got := w.refusal(t); got != apphostmod.ErrContractRequestsOff.Error() {
		t.Fatalf("answered %q; want %q", got, apphostmod.ErrContractRequestsOff)
	}
}

func objectsOf(p []*auth.Permit) (list []astral.Object) {
	for _, permit := range p {
		list = append(list, permit)
	}
	return list
}

// awaitRequest waits until SwarmState holds one request and returns its ID.
func awaitRequest(t *testing.T, swarm *testSwarm) *astral.ObjectID {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ids, _ := swarm.state.SwarmEntries(user.EntryRequests); len(ids) == 1 {
			return ids[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no request filed")
	return nil
}

func receiveAll(t *testing.T, w *recordingWriter) (list []astral.Object) {
	t.Helper()
	w.mu.Lock()
	r := channel.NewReceiver(bytes.NewReader(w.buf.Bytes()))
	w.mu.Unlock()
	for {
		obj, err := r.Receive()
		if errors.Is(err, io.EOF) {
			return list
		}
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		list = append(list, obj)
	}
}

// An Approval of a request declined before the host polls is not carried
// out: a Decline wins.
func TestDeclinedApprovalIsNotCarriedOut(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	reqID := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
	swarm.answer(t, reqID, true)
	swarm.answer(t, reqID, false)

	mustPoll(t, host)
	if n := len(swarm.contracts()); n != 0 {
		t.Fatalf("signed %v contracts for a declined request", n)
	}
}
