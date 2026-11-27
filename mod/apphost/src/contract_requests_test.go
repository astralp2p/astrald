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

// fakeSwarmView is a user.SwarmView over a fakeSwarmState: fulfilled beats
// declined beats expired beats offered beats pending. An offer is a contract
// only its issuer signed, given to the request's filer. Every issuer may
// answer except those in refused.
type fakeSwarmView struct {
	state      *fakeSwarmState
	now        func() time.Time
	membership *user.Membership
	refused    map[string]bool
}

var _ hostSwarmView = &fakeSwarmView{}

func (v *fakeSwarmView) RequestStatus(req *astral.ObjectID) user.RequestStatus {
	for _, c := range v.ContractsOf(req) {
		if c.IssuerSig != nil && c.SubjectSig != nil {
			return user.RequestFulfilled
		}
	}
	for _, obj := range v.state.objects(user.EntryDeclines) {
		if obj.(*user.SignedDecline).RequestID.IsEqual(req) {
			return user.RequestDeclined
		}
	}
	obj, err := v.state.SwarmEntry(req)
	r, ok := obj.(*user.SignedContractRequest)
	if err != nil || !ok {
		return user.RequestPending
	}
	if r.Expired(v.now()) {
		return user.RequestExpired
	}
	for _, c := range v.ContractsOf(req) {
		if c.SubjectSig == nil && c.Subject.IsEqual(r.FiledBy) {
			return user.RequestOffered
		}
	}
	return user.RequestPending
}

func (v *fakeSwarmView) ContractsOf(req *astral.ObjectID) (list []*auth.SignedContract) {
	for _, obj := range v.state.objects(user.EntryContracts) {
		if c := obj.(*auth.SignedContract); c.RequestID != nil && c.RequestID.IsEqual(req) {
			list = append(list, c)
		}
	}
	return list
}

func (v *fakeSwarmView) Membership(*astral.Identity) (*user.Membership, bool) {
	return v.membership, v.membership != nil
}

func (v *fakeSwarmView) MayAnswer(signer *astral.Identity, _ *astral.ObjectID) bool {
	return !v.refused[signer.String()]
}

// fakeSigner signs requests and contracts with a marker: a request's
// signature holds its signable hash, a contract's the signer's identity.
type fakeSigner struct{}

func (fakeSigner) Sign(_ *astral.Context, _ *crypto.PublicKey, obj crypto.SignableTextObject) (*crypto.Signature, error) {
	return &crypto.Signature{Scheme: "test", Data: astral.Bytes16(obj.SignableHash())}, nil
}

func (fakeSigner) SignContract(_ *astral.Context, c *auth.SignedContract) error {
	c.IssuerSig = markSig(c.Issuer)
	c.SubjectSig = markSig(c.Subject)
	return nil
}

func (fakeSigner) SignSubject(_ *astral.Context, c *auth.SignedContract) (*crypto.Signature, error) {
	if c.SubjectSig != nil {
		return nil, auth.ErrAlreadySigned
	}
	c.SubjectSig = markSig(c.Subject)
	return c.SubjectSig, nil
}

// Verify accepts a signature that holds obj's signable hash, as fakeSigner.Sign makes it.
func (fakeSigner) Verify(_ *crypto.PublicKey, sig *crypto.Signature, obj crypto.SignableTextObject) error {
	if sig == nil || !bytes.Equal(sig.Data, obj.SignableHash()) {
		return errors.New("bad signature")
	}
	return nil
}

func markSig(id *astral.Identity) *crypto.Signature {
	return &crypto.Signature{Scheme: "test", Data: astral.Bytes16(id.String())}
}

// testSwarm is a shared SwarmState, its view, its root key and a clock every
// node reads.
type testSwarm struct {
	state *fakeSwarmState
	view  *fakeSwarmView
	root  *astral.Identity
	mu    sync.Mutex
	clock time.Time
}

func newTestSwarm() *testSwarm {
	s := &testSwarm{state: newFakeSwarmState(), root: astral.GenerateIdentity(), clock: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)}
	s.view = &fakeSwarmView{state: s.state, now: s.now, refused: map[string]bool{}}
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
	return s.hostAs(astral.GenerateIdentity())
}

// hostAs returns the contract requests of node in s, as after a restart.
func (s *testSwarm) hostAs(node *astral.Identity) *contractRequests {
	cr := newContractRequests(node, &hostState{fakeSwarmState: s.state, node: node, now: s.now}, s.view, fakeSigner{})
	cr.now = s.now
	cr.verifier = fakeSigner{}
	cr.root = func() *astral.Identity { return s.root }
	return cr
}

// request returns the stored ContractRequest req.
func (s *testSwarm) request(t *testing.T, req *astral.ObjectID) *user.SignedContractRequest {
	t.Helper()
	obj, err := s.state.SwarmEntry(req)
	if err != nil {
		t.Fatalf("request %v: %v", req, err)
	}
	return obj.(*user.SignedContractRequest)
}

// offerBody returns issuer's unsigned offer for req: every requested permit
// at one more Delegation, to the filer, expiring after usermod.OfferValidity.
func (s *testSwarm) offerBody(t *testing.T, issuer *astral.Identity, req *astral.ObjectID) *auth.SignedContract {
	t.Helper()
	r := s.request(t, req)
	var permits []*auth.Permit
	for _, p := range r.Permits {
		deeper := *p
		deeper.Delegation++
		permits = append(permits, &deeper)
	}
	return &auth.SignedContract{Contract: &auth.Contract{
		Issuer: issuer, Subject: r.FiledBy, Permits: permits, RequestID: req,
		ExpiresAt: astral.Time(s.now().Add(usermod.OfferValidity)),
	}}
}

// publish signs sc as its issuer only, adds it and returns its entry ID.
func (s *testSwarm) publish(t *testing.T, sc *auth.SignedContract) *astral.ObjectID {
	t.Helper()
	sc.IssuerSig = markSig(sc.Issuer)
	if err := s.state.add(sc); err != nil {
		t.Fatalf("add offer: %v", err)
	}
	id, err := astral.ResolveObjectID(sc)
	if err != nil {
		t.Fatalf("offer id: %v", err)
	}
	return id
}

// offer publishes issuer's offer for req and returns its entry ID.
func (s *testSwarm) offer(t *testing.T, issuer *astral.Identity, req *astral.ObjectID) *astral.ObjectID {
	t.Helper()
	return s.publish(t, s.offerBody(t, issuer, req))
}

// answer adds a Privileged node's offer (approve) or Decline of req and
// returns the offer's entry ID, or nil for a Decline.
func (s *testSwarm) answer(t *testing.T, req *astral.ObjectID, approve bool) *astral.ObjectID {
	t.Helper()
	issuer := astral.GenerateIdentity()
	if approve {
		return s.offer(t, issuer, req)
	}
	if err := s.state.add(&user.SignedDecline{Decline: user.NewDecline(issuer, req), IssuerSig: markSig(issuer)}); err != nil {
		t.Fatalf("add decline: %v", err)
	}
	return nil
}

// contracts returns the contracts stored in s.
func (s *testSwarm) contracts() (list []*auth.SignedContract) {
	for _, obj := range s.state.objects(user.EntryContracts) {
		list = append(list, obj.(*auth.SignedContract))
	}
	return list
}

// signedBy returns the stored contracts both parties signed whose issuer is issuer.
func (s *testSwarm) signedBy(issuer *astral.Identity) (list []*auth.SignedContract) {
	for _, c := range s.contracts() {
		if c.SubjectSig != nil && c.Issuer.IsEqual(issuer) {
			list = append(list, c)
		}
	}
	return list
}

// cosigned returns the stored offers to host that host co-signed.
func (s *testSwarm) cosigned(host *astral.Identity) (list []*auth.SignedContract) {
	for _, c := range s.contracts() {
		if c.SubjectSig != nil && c.Subject.IsEqual(host) && !c.Issuer.IsEqual(host) {
			list = append(list, c)
		}
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

	restarted := swarm.hostAs(host.node)
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

// Done (criterion 14): an app grant is two contracts. The host co-signs the
// approver's offer, which makes the approver's contract to the host with the
// requested permits one Delegation deeper, and signs its own contract to the
// identity the request names with the requested permits, the request's entry
// ID as its RequestID, expiring no later than the approver's. Each is signed
// once.
//
// note: ported from TestApprovedRequestSignsContractForNamedIdentity; a
// contract request is answered by an offer since the two-signature model.
// That expelling the host then denies the app is the auth walk's
// (mod/auth TestSwarmWalkStopsAtAnExpelledParty, "expelled host").
func TestOfferedRequestMakesTheTwoContractGrant(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	app := astral.GenerateIdentity()
	reqID := mustFile(t, host, app, walletPermits())

	mustPoll(t, host)
	if n := len(swarm.contracts()); n != 0 {
		t.Fatalf("signed %v contracts before an offer", n)
	}

	approver := astral.GenerateIdentity()
	swarm.offer(t, approver, reqID)
	mustPoll(t, host)
	mustPoll(t, host)

	granted, mine := swarm.signedBy(approver), swarm.signedBy(host.node)
	if len(granted) != 1 || len(mine) != 1 || len(swarm.contracts()) != 3 {
		t.Fatalf("contracts %v; want the offer, its completed form and the app's contract", swarm.contracts())
	}
	g, c := granted[0], mine[0]
	switch {
	case !g.Subject.IsEqual(host.node) || g.Permits[0].Delegation != 1 || !g.RequestID.IsEqual(reqID):
		t.Errorf("approver's contract to %v at delegation %v for %v; want to the host at 1 for the request", g.Subject, g.Permits[0].Delegation, g.RequestID)
	case !c.Subject.IsEqual(app):
		t.Errorf("subject %v; want the app the request names", c.Subject)
	case !c.RequestID.IsEqual(reqID):
		t.Errorf("RequestID %v; want %v", c.RequestID, reqID)
	case !samePermits(&user.ContractRequest{Permits: c.Permits}, &user.ContractRequest{Permits: walletPermits()}):
		t.Errorf("permits %v; want the requested ones", c.Permits)
	case c.ExpiresAt.Time().After(g.ExpiresAt.Time()):
		t.Errorf("app contract expires %v, after the approver's %v", c.ExpiresAt, g.ExpiresAt)
	}

	got, err := host.Outcome(reqID)
	if err != nil || got != c {
		t.Fatalf("outcome %v, %v; want the signed contract", got, err)
	}
}

// A host restarted after co-signing signs the app's contract from the stored
// co-signed contract, and co-signs no second offer.
func TestRestartedHostSignsTheAppContractOnce(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	app := astral.GenerateIdentity()
	reqID := mustFile(t, host, app, walletPermits())
	offer := swarm.offerBody(t, astral.GenerateIdentity(), reqID)
	swarm.publish(t, offer)
	done := &auth.SignedContract{Contract: offer.Contract, IssuerSig: offer.IssuerSig, SubjectSig: markSig(host.node)}
	if err := swarm.state.add(done); err != nil {
		t.Fatal(err)
	}
	swarm.offer(t, astral.GenerateIdentity(), reqID)

	restarted := swarm.hostAs(host.node)
	mustPoll(t, restarted)
	if n, m := len(swarm.cosigned(host.node)), len(swarm.signedBy(host.node)); n != 1 || m != 1 {
		t.Fatalf("%v co-signed offers and %v app contracts; want 1 and 1", n, m)
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

	if c := swarm.signedBy(host.node); len(c) != 1 || !c[0].ExpiresAt.Time().Equal(ends) {
		t.Fatalf("contracts %v; want one expiring with the membership at %v", c, ends)
	}
}

// The app's contract never outlives the approver's contract to the host.
func TestAppContractExpiresWithTheOffer(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	reqID := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
	offer := swarm.offerBody(t, astral.GenerateIdentity(), reqID)
	ends := swarm.now().Add(72 * time.Hour)
	offer.ExpiresAt = astral.Time(ends)
	swarm.publish(t, offer)
	mustPoll(t, host)

	if c := swarm.signedBy(host.node); len(c) != 1 || !c[0].ExpiresAt.Time().Equal(ends) {
		t.Fatalf("contracts %v; want one expiring with the offer at %v", c, ends)
	}
}

// Done: another app cannot reuse that offer. App B's request stays
// pending, B gets no contract, and A's contract names A alone.
func TestAnotherAppCannotReuseAnOffer(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	appA, appB := astral.GenerateIdentity(), astral.GenerateIdentity()
	reqA := mustFile(t, host, appA, walletPermits())
	reqB := mustFile(t, host, appB, walletPermits())
	if reqA.IsEqual(reqB) {
		t.Fatal("app B got app A's request")
	}

	swarm.answer(t, reqA, true)
	mustPoll(t, host)

	contracts := swarm.signedBy(host.node)
	if len(contracts) != 1 || !contracts[0].Subject.IsEqual(appA) || !contracts[0].RequestID.IsEqual(reqA) {
		t.Fatalf("contracts %v; want only app A's, naming A's request", contracts)
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
// host does not co-sign an offer for the old host's request, and signs once
// its own request is offered.
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
	if n := len(swarm.signedBy(newHost.node)) + len(swarm.cosigned(newHost.node)); n != 0 {
		t.Fatalf("the new host signed on the old host's offer: %v contracts", n)
	}

	swarm.answer(t, newReq, true)
	mustPoll(t, newHost)
	c, err := newHost.Outcome(newReq)
	if err != nil || c == nil || !c.Issuer.IsEqual(newHost.node) || !c.RequestID.IsEqual(newReq) {
		t.Fatalf("new host outcome %v, %v; want its own contract on its own offer", c, err)
	}
}

// Criterion 15: the host co-signs at most one offer per request. Of several
// that qualify it takes the root key's, then the one that expires last, then
// the one with the lowest entry ID; a second approver's offer that arrives
// after it co-signed stays unused.
func TestHostCoSignsOneOfferPerRequest(t *testing.T) {
	later := func(s *testSwarm, sc *auth.SignedContract) { sc.ExpiresAt = astral.Time(s.now().Add(time.Hour)) }
	for _, c := range []struct {
		name string
		// mine, theirs: the offer the host should take, and the other one
		mine, theirs func(s *testSwarm, sc *auth.SignedContract)
		rootMine     bool
	}{
		{"the root key's first", later, func(*testSwarm, *auth.SignedContract) {}, true},
		{"then the later expiry", func(*testSwarm, *auth.SignedContract) {}, later, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			swarm := newTestSwarm()
			host := swarm.host()
			reqID := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
			mineIssuer := astral.GenerateIdentity()
			if c.rootMine {
				mineIssuer = swarm.root
			}
			mine, theirs := swarm.offerBody(t, mineIssuer, reqID), swarm.offerBody(t, astral.GenerateIdentity(), reqID)
			c.mine(swarm, mine)
			c.theirs(swarm, theirs)
			swarm.publish(t, theirs)
			swarm.publish(t, mine)
			mustPoll(t, host)
			swarm.offer(t, astral.GenerateIdentity(), reqID)
			mustPoll(t, host)

			got := swarm.cosigned(host.node)
			if len(got) != 1 || !got[0].Issuer.IsEqual(mineIssuer) {
				t.Fatalf("co-signed %v; want only the offer of %v", got, mineIssuer)
			}
			if n := len(swarm.signedBy(host.node)); n != 1 {
				t.Fatalf("signed %v app contracts; want 1", n)
			}
		})
	}

	swarm := newTestSwarm()
	host := swarm.host()
	reqID := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
	a, b := swarm.offer(t, astral.GenerateIdentity(), reqID), swarm.offer(t, astral.GenerateIdentity(), reqID)
	lowest := a
	if b.String() < a.String() {
		lowest = b
	}
	obj, _ := swarm.state.SwarmEntry(lowest)
	mustPoll(t, host)
	if got := swarm.cosigned(host.node); len(got) != 1 || !got[0].Issuer.IsEqual(obj.(*auth.SignedContract).Issuer) {
		t.Fatalf("co-signed %v; want the offer with the lowest entry ID", got)
	}
}

// Criteria 15, 18: the host co-signs only an offer for its own live request:
// none for a stale offer (its request expired, declined or superseded by a
// later request with the same permits, or the offer itself expired), nor for
// an offer wider than the request, given to another node, of a request
// another node filed, from an issuer that may not answer it, or of a request
// in SwarmState that this node did not sign.
func TestHostRefusesStaleAndForeignOffers(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(t *testing.T, s *testSwarm, host *contractRequests) *astral.ObjectID
	}{
		{"an expired request", func(t *testing.T, s *testSwarm, host *contractRequests) *astral.ObjectID {
			req := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
			s.offer(t, astral.GenerateIdentity(), req)
			s.advance(user.RequestTimeout + time.Second)
			return req
		}},
		{"a declined request", func(t *testing.T, s *testSwarm, host *contractRequests) *astral.ObjectID {
			req := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
			s.offer(t, astral.GenerateIdentity(), req)
			s.answer(t, req, false)
			return req
		}},
		{"a superseded request", func(t *testing.T, s *testSwarm, host *contractRequests) *astral.ObjectID {
			app := astral.GenerateIdentity()
			req := mustFile(t, host, app, walletPermits())
			s.offer(t, astral.GenerateIdentity(), req)
			s.advance(time.Minute)
			if _, err := host.state.FileContractRequest(astral.NewContext(nil), app, walletPermits()); err != nil {
				t.Fatal(err)
			}
			return req
		}},
		{"an expired offer", func(t *testing.T, s *testSwarm, host *contractRequests) *astral.ObjectID {
			req := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
			offer := s.offerBody(t, astral.GenerateIdentity(), req)
			offer.ExpiresAt = astral.Time(s.now())
			s.publish(t, offer)
			return req
		}},
		{"a wider offer", func(t *testing.T, s *testSwarm, host *contractRequests) *astral.ObjectID {
			req := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
			offer := s.offerBody(t, astral.GenerateIdentity(), req)
			offer.Permits = append(offer.Permits, &auth.Permit{Action: "mod.auth.see_objects_action", Delegation: 1})
			s.publish(t, offer)
			return req
		}},
		{"an offer at the requested depth", func(t *testing.T, s *testSwarm, host *contractRequests) *astral.ObjectID {
			req := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
			offer := s.offerBody(t, astral.GenerateIdentity(), req)
			offer.Permits[0].Delegation = 0
			s.publish(t, offer)
			return req
		}},
		{"an offer to another node", func(t *testing.T, s *testSwarm, host *contractRequests) *astral.ObjectID {
			req := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
			offer := s.offerBody(t, astral.GenerateIdentity(), req)
			offer.Subject = astral.GenerateIdentity()
			s.publish(t, offer)
			return req
		}},
		{"another node's request", func(t *testing.T, s *testSwarm, host *contractRequests) *astral.ObjectID {
			req := mustFile(t, s.host(), astral.GenerateIdentity(), walletPermits())
			offer := s.offerBody(t, astral.GenerateIdentity(), req)
			offer.Subject = host.node
			s.publish(t, offer)
			return nil
		}},
		{"an issuer that may not answer", func(t *testing.T, s *testSwarm, host *contractRequests) *astral.ObjectID {
			req := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
			issuer := astral.GenerateIdentity()
			s.view.refused[issuer.String()] = true
			s.offer(t, issuer, req)
			return req
		}},
		{"a request this node did not sign", func(t *testing.T, s *testSwarm, host *contractRequests) *astral.ObjectID {
			body := user.NewContractRequest(host.node, astral.GenerateIdentity(), walletPermits())
			forged := &user.SignedContractRequest{ContractRequest: body, FiledBySig: markSig(host.node)}
			if err := s.state.add(forged); err != nil {
				t.Fatal(err)
			}
			req, _ := astral.ResolveObjectID(forged)
			s.offer(t, astral.GenerateIdentity(), req)
			return nil
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			swarm := newTestSwarm()
			host := swarm.host()
			c.setup(t, swarm, host)
			mustPoll(t, host)
			if n, m := len(swarm.cosigned(host.node)), len(swarm.signedBy(host.node)); n != 0 || m != 0 {
				t.Fatalf("co-signed %v offers and signed %v app contracts; want none", n, m)
			}
		})
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

// A Decline and an expiry reach the waiting app, and the app then files a
// fresh request. A Decline after the contract leaves the contract: it only
// closes the request (PR 4 decision Q1).
func TestDeclineAndExpiryReachTheApp(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	ctx := astral.NewContext(nil)

	declinedApp := astral.GenerateIdentity()
	declined := mustFile(t, host, declinedApp, walletPermits())
	swarm.answer(t, declined, false)
	mustPoll(t, host)

	signed := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
	swarm.answer(t, signed, true)
	mustPoll(t, host)
	swarm.answer(t, signed, false)
	mustPoll(t, host)
	if c, err := host.Outcome(signed); c == nil || err != nil {
		t.Fatalf("outcome after a late Decline %v, %v; want the signed contract", c, err)
	}

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

// Wait returns once a poll co-signs the offer and signs the app's contract.
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
// then the contract once the host co-signed an offer and signed it.
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

// Criterion 18: an offer for a request declined before the host polls is not
// co-signed: a Decline closes the request.
//
// note: ported from TestDeclinedApprovalIsNotCarriedOut.
func TestDeclinedOfferIsNotCoSigned(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	reqID := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
	swarm.answer(t, reqID, true)
	swarm.answer(t, reqID, false)

	mustPoll(t, host)
	if n := len(swarm.contracts()); n != 1 {
		t.Fatalf("SwarmState holds %v contracts for a declined request; want the offer alone", n)
	}
	if _, err := host.Outcome(reqID); !errors.Is(err, apphostmod.ErrContractDeclined) {
		t.Fatalf("outcome %v; want declined", err)
	}
}

// Criterion 18: a Decline that reaches the host after it co-signed an offer
// takes nothing back. The host, restarted before it signed the app's
// contract, signs it from the co-signed contract, which both parties signed,
// and co-signs no other offer.
func TestHostCarriesOutAContractCoSignedBeforeADecline(t *testing.T) {
	swarm := newTestSwarm()
	host := swarm.host()
	reqID := mustFile(t, host, astral.GenerateIdentity(), walletPermits())
	offer := swarm.offerBody(t, astral.GenerateIdentity(), reqID)
	swarm.publish(t, offer)
	done := &auth.SignedContract{Contract: offer.Contract, IssuerSig: offer.IssuerSig, SubjectSig: markSig(host.node)}
	if err := swarm.state.add(done); err != nil {
		t.Fatal(err)
	}
	swarm.answer(t, reqID, false)
	swarm.offer(t, astral.GenerateIdentity(), reqID)

	restarted := swarm.hostAs(host.node)
	mustPoll(t, restarted)
	if n, m := len(swarm.cosigned(host.node)), len(swarm.signedBy(host.node)); n != 1 || m != 1 {
		t.Fatalf("%v co-signed offers and %v app contracts; want 1 and 1", n, m)
	}
	if c, err := restarted.Outcome(reqID); c == nil || err != nil {
		t.Fatalf("outcome %v, %v; want the signed contract", c, err)
	}
}
