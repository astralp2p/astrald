package user

import (
	"errors"
	usermod "github.com/astralp2p/astrald/mod/user"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// fakeSwarmState is an in-memory usermod.SwarmState that stores every entry
// it is given: the merge check is out of G2's scope.
type fakeSwarmState struct {
	mu      sync.Mutex
	entries map[astral.ObjectID]astral.Object
	reads   int

	// failList fails SwarmEntries of that kind; failRead fails SwarmEntry
	// of every entry of that kind. The empty kind fails nothing.
	failList, failRead user.EntryKind
}

var _ usermod.SwarmState = &fakeSwarmState{}

func newFakeSwarmState() *fakeSwarmState {
	return &fakeSwarmState{entries: map[astral.ObjectID]astral.Object{}}
}

func (f *fakeSwarmState) AddSwarmEntry(_ *astral.Context, obj astral.Object) error {
	if _, ok := user.EntryKindOf(obj); !ok {
		return errors.New("not an entry")
	}
	id, err := astral.ResolveObjectID(obj)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.entries[*id]; !ok {
		f.entries[*id] = obj
	}
	return nil
}

func (f *fakeSwarmState) SwarmEntry(id *astral.ObjectID) (astral.Object, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	obj, ok := f.entries[*id]
	if !ok {
		return nil, errors.New("not stored")
	}
	if k, _ := user.EntryKindOf(obj); f.failRead != "" && k == f.failRead {
		return nil, errors.New("read failed")
	}
	return obj, nil
}

func (f *fakeSwarmState) SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error) {
	if !kind.IsValid() {
		return nil, errors.New("invalid kind")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.failList != "" && kind == f.failList {
		return nil, errors.New("list failed")
	}
	var ids []*astral.ObjectID
	for id, obj := range f.entries {
		if k, _ := user.EntryKindOf(obj); k == kind {
			ids = append(ids, &id)
		}
	}
	return ids, nil
}

func (f *fakeSwarmState) SwarmHead() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return uint64(len(f.entries))
}

// viewFixture is a swarm on a fixed clock: the user key (root), a Privileged
// Laptop and Desktop, a Trusted Phone, a Member Pi and App A hosted on the Pi.
type viewFixture struct {
	t     *testing.T
	state *fakeSwarmState
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
		t: t, state: newFakeSwarmState(), now: viewAt10,
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
	if err := f.state.AddSwarmEntry(nil, obj); err != nil {
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

// member returns a membership valid for a year from 10:00.
func (f *viewFixture) member(issuer, subject *astral.Identity, tier user.Tier, approval *astral.ObjectID) *auth.SignedContract {
	return f.memberUntil(issuer, subject, tier, approval, viewAt10.Add(365*24*time.Hour))
}

func (f *viewFixture) memberUntil(issuer, subject *astral.Identity, tier user.Tier, approval *astral.ObjectID, expires time.Time) *auth.SignedContract {
	f.t.Helper()
	c, err := user.NewMembership(issuer, subject, tier, approval, time.Hour)
	if err != nil {
		f.t.Fatal(err)
	}
	c.ExpiresAt = astral.Time(expires)
	return &auth.SignedContract{Contract: c, IssuerSig: viewSig()}
}

// appContract returns the host's contract to its app carrying out approval.
func (f *viewFixture) appContract(host, app *astral.Identity, approval *astral.ObjectID) *auth.SignedContract {
	c := &auth.Contract{
		Issuer: host, Subject: app, ApprovalID: approval,
		Permits:   []*auth.Permit{{Action: "mod.services.discover_action"}},
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

func hasViewApproval(list []*user.SignedApproval, id *astral.ObjectID) bool {
	for _, a := range list {
		if aid, _ := astral.ResolveObjectID(a); aid != nil && aid.IsEqual(id) {
			return true
		}
	}
	return false
}
