package user

import (
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
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
