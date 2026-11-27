package user

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// storeState is the reading part of usermod.SwarmState over the real store of one test node, whose scripted
// checker accepts every entry: the entry check is out of the view's scope. It counts the reads of the entry lists.
type storeState struct {
	usermod.SwarmState
	node  *stateNode
	mu    sync.Mutex
	lists int
}

func newStoreState(t testing.TB) *storeState {
	return &storeState{node: newStateNode(t, newSwarmScript())}
}

// merge stores obj in the node's store.
func (s *storeState) merge(_ *astral.Context, obj astral.Object) error {
	return s.node.store.merge(s.node.store.ctx, obj)
}

func (s *storeState) SwarmEntry(id *astral.ObjectID) (astral.Object, error) {
	return s.node.store.set.Entry(id)
}

func (s *storeState) SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error) {
	s.mu.Lock()
	s.lists++
	s.mu.Unlock()
	return s.node.store.set.Entries(kind)
}

func (s *storeState) SwarmHead() uint64 { return s.node.store.set.Head() }

// reads returns how many times the view listed an entry kind.
func (s *storeState) reads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lists
}

// viewFixture is a swarm on a fixed clock: the user key (root), a Privileged
// Laptop and Desktop, a Trusted Phone, a Member Pi and App A hosted on the Pi.
type viewFixture struct {
	t     *testing.T
	state *storeState
	view  *swarmView
	now   time.Time

	user, laptop, desktop, phone, pi, appA *astral.Identity
}

// viewAt10 is 10:00 on the day of the working paper's examples.
var viewAt10 = time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)

func viewClock(hh, mm int) time.Time {
	return viewAt10.Add(time.Duration(hh-10)*time.Hour + time.Duration(mm)*time.Minute)
}

func newViewFixture(t *testing.T) *viewFixture {
	t.Helper()
	f := &viewFixture{
		t: t, state: newStoreState(t), now: viewAt10,
		user: astral.GenerateIdentity(), laptop: astral.GenerateIdentity(),
		desktop: astral.GenerateIdentity(), phone: astral.GenerateIdentity(),
		pi: astral.GenerateIdentity(), appA: astral.GenerateIdentity(),
	}
	f.view = newSwarmView(f.state, func() *astral.Identity { return f.user }, func() time.Time { return f.now })
	f.add(f.member(f.user, f.laptop, user.TierPrivilegedMember, nil))
	f.add(f.member(f.user, f.desktop, user.TierPrivilegedMember, nil))
	f.add(f.member(f.user, f.phone, user.TierTrusted, nil))
	f.add(f.member(f.user, f.pi, user.TierMember, nil))
	return f
}

// add stores obj and returns its entry ID.
func (f *viewFixture) add(obj astral.Object) *astral.ObjectID {
	f.t.Helper()
	if err := f.state.merge(nil, obj); err != nil {
		f.t.Fatalf("add %T: %v", obj, err)
	}
	id, err := astral.ResolveObjectID(obj)
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func viewSig() *crypto.Signature {
	return &crypto.Signature{Scheme: "test", Data: astral.Bytes16{1}}
}

func (f *viewFixture) contractRequest(filedBy, identity *astral.Identity, filedAt time.Time) *user.SignedContractRequest {
	r := user.NewContractRequest(filedBy, identity, []*auth.Permit{{Action: "mod.services.discover_action"}})
	r.FiledAt, r.Timeout = astral.Time(filedAt), user.RequestDeadline(astral.Time(filedAt))
	return &user.SignedContractRequest{ContractRequest: r, FiledBySig: viewSig()}
}

func (f *viewFixture) joinRequest(joiner, carrier *astral.Identity, tier user.Tier, filedAt time.Time) *user.SignedJoinRequest {
	r := user.NewJoinRequest(joiner, carrier, tier)
	r.FiledAt, r.Timeout = astral.Time(filedAt), user.RequestDeadline(astral.Time(filedAt))
	return &user.SignedJoinRequest{JoinRequest: r, JoinerSig: viewSig()}
}

func (f *viewFixture) expelRequest(filedBy, subject *astral.Identity, filedAt time.Time) *user.SignedExpelRequest {
	r := user.NewExpelRequest(filedBy, subject)
	r.FiledAt, r.Timeout = astral.Time(filedAt), user.RequestDeadline(astral.Time(filedAt))
	return &user.SignedExpelRequest{ExpelRequest: r, FiledBySig: viewSig()}
}

func (f *viewFixture) approval(issuer *astral.Identity, req *astral.ObjectID) *user.SignedApproval {
	return &user.SignedApproval{Approval: user.NewApproval(issuer, req), IssuerSig: viewSig()}
}

func (f *viewFixture) decline(issuer *astral.Identity, req *astral.ObjectID) *user.SignedDecline {
	return &user.SignedDecline{Decline: user.NewDecline(issuer, req), IssuerSig: viewSig()}
}

func (f *viewFixture) expulsion(issuer, subject *astral.Identity) *user.SignedExpulsion {
	e := &user.Expulsion{Issuer: issuer, Subject: subject, ExpelledAt: astral.Time(f.now)}
	return &user.SignedExpulsion{Expulsion: e, IssuerSig: viewSig()}
}

// member returns a membership valid for a year from 10:00, signed by both
// parties, that names req as its RequestID.
func (f *viewFixture) member(issuer, subject *astral.Identity, tier user.Tier, req *astral.ObjectID) *auth.SignedContract {
	return f.memberUntil(issuer, subject, tier, req, viewAt10.Add(365*24*time.Hour))
}

func (f *viewFixture) memberUntil(issuer, subject *astral.Identity, tier user.Tier, req *astral.ObjectID, expires time.Time) *auth.SignedContract {
	f.t.Helper()
	c, err := user.NewMembershipUntil(issuer, subject, tier, req, astral.Time(expires))
	if err != nil {
		f.t.Fatal(err)
	}
	return &auth.SignedContract{Contract: c, IssuerSig: viewSig(), SubjectSig: viewSig()}
}

// appContract returns the host's contract to its app, signed by both, that
// carries out req.
func (f *viewFixture) appContract(host, app *astral.Identity, req *astral.ObjectID) *auth.SignedContract {
	c := &auth.Contract{
		Issuer: host, Subject: app, RequestID: req,
		Permits:   []*auth.Permit{{Action: "mod.services.discover_action"}},
		ExpiresAt: astral.Time(viewAt10.Add(24 * time.Hour)),
	}
	return &auth.SignedContract{Contract: c, IssuerSig: viewSig(), SubjectSig: viewSig()}
}

// offer returns issuer's offer, signed by issuer only, answering req, the
// ContractRequest with entry ID reqID that f.contractRequest builds: its
// permit at Delegation 1 to the filer, valid for a day from 10:00.
func (f *viewFixture) offer(issuer *astral.Identity, req *user.SignedContractRequest, reqID *astral.ObjectID) *auth.SignedContract {
	c := &auth.Contract{
		Issuer: issuer, Subject: req.FiledBy, RequestID: reqID,
		Permits:   []*auth.Permit{{Action: "mod.services.discover_action", Delegation: 1}},
		ExpiresAt: astral.Time(viewAt10.Add(24 * time.Hour)),
	}
	return &auth.SignedContract{Contract: c, IssuerSig: viewSig()}
}

func (f *viewFixture) wantStatus(req *astral.ObjectID, want user.RequestStatus) {
	f.t.Helper()
	if got := f.view.RequestStatus(req); got != want {
		f.t.Fatalf("RequestStatus at %v = %v, want %v", f.now.Format("15:04:05.000000000"), got, want)
	}
}

func (f *viewFixture) wantMembership(id *astral.Identity, want bool) {
	f.t.Helper()
	if _, got := f.view.Membership(id); got != want {
		f.t.Fatalf("Membership = %v, want %v", got, want)
	}
}

// useSwarm gives mod a SwarmView over an in-memory SwarmState holding, for
// each of members, a Member-tier membership userID signed, with userID as the
// root key. It returns the state, so a test adds entries later.
//
// note: the view trusts stored entries, so the signatures are placeholders.
func useSwarm(t testing.TB, mod *Module, userID *astral.Identity, members ...*astral.Identity) *storeState {
	t.Helper()
	state := newStoreState(t)
	for _, m := range members {
		addMembership(t, state, userID, m)
	}
	mod.swarmView = newSwarmView(state, func() *astral.Identity { return userID }, time.Now)
	return state
}

// addMembership stores subject's Member-tier membership, signed by issuer.
func addMembership(t testing.TB, state *storeState, issuer, subject *astral.Identity) *auth.SignedContract {
	t.Helper()
	c, err := user.NewMembership(issuer, subject, user.TierMember, nil, time.Hour)
	if err != nil {
		t.Fatalf("membership: %v", err)
	}
	sc := &auth.SignedContract{Contract: c, IssuerSig: viewSig(), SubjectSig: viewSig()}
	if err = state.merge(nil, sc); err != nil {
		t.Fatalf("store membership: %v", err)
	}
	return sc
}

// contractState is a SwarmState whose contracts are the ones contracts
// answers, so a test that stages members in a fake auth index sees them as
// SwarmState members. Its head moves on every read, so the view reads the
// staged set each time.
type contractState struct {
	usermod.SwarmState
	contracts func() ([]*auth.SignedContract, error)
	head      atomic.Uint64
	// store holds the Expulsions: the module's own store, as its AddExpulsion writes them there.
	store *swarmStore
}

func (s *contractState) SwarmEntry(id *astral.ObjectID) (astral.Object, error) {
	found, _ := s.contracts()
	for _, c := range found {
		if cid, err := astral.ResolveObjectID(c); err == nil && cid.IsEqual(id) {
			return c, nil
		}
	}
	return s.store.set.Entry(id)
}

func (s *contractState) SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error) {
	if kind == user.EntryExpels {
		return s.store.set.Entries(kind)
	}
	if kind != user.EntryContracts {
		return nil, nil
	}
	found, err := s.contracts()
	if err != nil {
		return nil, err
	}
	var ids []*astral.ObjectID
	for _, c := range found {
		if id, err := astral.ResolveObjectID(c); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *contractState) SwarmHead() uint64 { return s.head.Add(1) }

// useQuerySwarm gives mod a SwarmView over q's staged contracts, with userID
// as the root key.
func useQuerySwarm(t testing.TB, mod *Module, userID *astral.Identity, q *memberQuery) {
	useContractSwarm(t, mod, userID, func() ([]*auth.SignedContract, error) {
		q.mu.Lock()
		defer q.mu.Unlock()
		return append([]*auth.SignedContract(nil), q.found...), q.err
	})
}

// useIndexSwarm gives mod a SwarmView over the contracts a seeds, with userID
// as the root key.
func useIndexSwarm(t testing.TB, mod *Module, userID *astral.Identity, a *indexAuth) {
	useContractSwarm(t, mod, userID, func() ([]*auth.SignedContract, error) {
		a.mu.Lock()
		defer a.mu.Unlock()
		return append([]*auth.SignedContract(nil), a.index...), nil
	})
}

// useContractSwarm gives mod a SwarmView over the contracts the function answers, with userID as the root key,
// and a real store for the Expulsions the view reads. A test whose module writes Expulsions itself shares the
// store with mod.swarmState (useStore).
func useContractSwarm(t testing.TB, mod *Module, userID *astral.Identity, contracts func() ([]*auth.SignedContract, error)) {
	t.Helper()
	store := newSwarmStore(realRules(func() *astral.Identity { return userID }))
	ctx := astral.NewContext(nil).WithIdentity(astral.GenerateIdentity()).IncludeZone(astral.ZoneNetwork)
	if _, err := store.open(ctx, newMemFiles(), astral.GenerateIdentity()); err != nil {
		t.Fatalf("open: %v", err)
	}
	mod.swarmView = newSwarmView(&contractState{contracts: contracts, store: store}, func() *astral.Identity { return userID }, time.Now)
}

// pullNoSwarm is a nodes module with no behaviour: a SwarmState pull on a module whose store is closed does
// nothing, and the tests that use it read the swarm from a SwarmView fixture.
type pullNoSwarm struct{ nodesmod.Module }

// useStore makes the store behind mod's contract view the one mod's own writes go to.
func useStore(mod *Module) {
	mod.swarmState = mod.swarmView.state.(*contractState).store
}

// banNode stores the Expulsion issuer signed on subject where mod's view reads it: in the in-memory state
// a test gave it, or in its real store.
func banNode(t testing.TB, mod *Module, issuer, subject *astral.Identity) {
	t.Helper()
	if err := storeBan(mod, sampleSignedValid(issuer, subject)); err != nil {
		t.Fatalf("store expulsion: %v", err)
	}
}

// storeBan merges signed where mod's view reads Expulsions.
func storeBan(mod *Module, signed *user.SignedExpulsion) error {
	switch state := mod.swarmView.state.(type) {
	case *storeState:
		return state.merge(nil, signed)
	case *contractState:
		return state.store.merge(state.store.ctx, signed)
	default:
		return mod.swarmState.merge(mod.swarmState.ctx, signed)
	}
}

// expulsionCount returns how many Expulsions mod's view holds.
func expulsionCount(t testing.TB, mod *Module) int {
	t.Helper()
	ids, err := mod.swarmView.state.SwarmEntries(user.EntryExpels)
	if err != nil {
		t.Fatalf("list expulsions: %v", err)
	}
	return len(ids)
}
