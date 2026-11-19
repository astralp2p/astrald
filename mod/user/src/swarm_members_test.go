package user

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// memberQuery returns the staged membership contracts that have not expired,
// or err, for any filter, as the auth index does.
type memberQuery struct {
	mu    sync.Mutex
	found []*auth.SignedContract
	err   error
	finds atomic.Uint64
}

func (q *memberQuery) WithIssuer(*astral.Identity) authmod.ContractQueryBuilder  { return q }
func (q *memberQuery) WithSubject(*astral.Identity) authmod.ContractQueryBuilder { return q }
func (q *memberQuery) WithAction(...astral.Object) authmod.ContractQueryBuilder  { return q }

func (q *memberQuery) Find(*astral.Context) ([]*auth.SignedContract, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.finds.Add(1)
	var active []*auth.SignedContract
	for _, c := range q.found {
		if c.ExpiresAt.Time().After(time.Now()) {
			active = append(active, c)
		}
	}
	return active, q.err
}

// stage replaces the staged contracts.
func (q *memberQuery) stage(found ...*auth.SignedContract) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.found = found
}

type memberAuth struct {
	authmod.Module
	query *memberQuery
}

func (a *memberAuth) SignedContracts() authmod.ContractQueryBuilder { return a.query }

// eventRecorder records every emitted event's data.
type eventRecorder struct {
	mu   sync.Mutex
	data []astral.Object
}

func (r *eventRecorder) Emit(data astral.Object) *events.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = append(r.data, data)
	return &events.Event{Data: data}
}

// take returns the recorded events and forgets them.
func (r *eventRecorder) take() []astral.Object {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := r.data
	r.data = nil
	return d
}

func memberContract(t testing.TB, issuer, subject *astral.Identity) *auth.SignedContract {
	t.Helper()
	return memberContractFor(t, issuer, subject, time.Hour)
}

func memberContractFor(t testing.TB, issuer, subject *astral.Identity, validity time.Duration) *auth.SignedContract {
	t.Helper()
	c, err := user.NewNodeContract(issuer, subject, false, validity)
	if err != nil {
		t.Fatal(err)
	}
	return &auth.SignedContract{Contract: c}
}

type memberEnv struct {
	mod    *Module
	query  *memberQuery
	events *eventRecorder
	issuer *astral.Identity
}

func newMemberEnv(t *testing.T) *memberEnv {
	issuer := astral.GenerateIdentity()
	mod := banModule(t, issuer)
	e := &memberEnv{mod: mod, query: &memberQuery{}, events: &eventRecorder{}, issuer: issuer}
	mod.Auth = &memberAuth{query: e.query}
	mod.Events = e.events
	mod.node = &identityNode{id: astral.GenerateIdentity()}
	mod.now = time.Now
	t.Cleanup(e.stopExpiry)
	return e
}

func (e *memberEnv) stopExpiry() {
	e.mod.swarm.mu.Lock()
	defer e.mod.swarm.mu.Unlock()
	e.mod.stopExpiry()
}

// One event per membership change, through the indexing hook and through an
// expulsion; none when the member set is unchanged.
func TestPublishSwarmEmitsOneEventPerChange(t *testing.T) {
	e := newMemberEnv(t)
	a := astral.GenerateIdentity()
	e.query.found = []*auth.SignedContract{memberContract(t, e.issuer, a)}
	e.mod.recordSwarm()
	if got := e.events.take(); len(got) != 0 {
		t.Fatalf("recording the first set emitted %v", got)
	}

	b := astral.GenerateIdentity()
	cb := memberContract(t, e.issuer, b)
	e.query.found = append(e.query.found, cb)
	e.mod.onContractIndexed(cb)
	got := e.events.take()
	if len(got) != 1 {
		t.Fatalf("%d events; want one joined", len(got))
	}
	if j, ok := got[0].(*user.SwarmMemberJoined); !ok || !j.NodeID.IsEqual(b) {
		t.Fatalf("got %v; want %v joined", got[0], b)
	}

	e.mod.onContractIndexed(cb)
	e.mod.publishSwarm()
	if got := e.events.take(); len(got) != 0 {
		t.Fatalf("an unchanged set emitted %v", got)
	}

	if err := e.mod.db.StoreExpulsion(sampleSigned(e.issuer, b)); err != nil {
		t.Fatal(err)
	}
	e.mod.publishSwarm()
	got = e.events.take()
	if len(got) != 1 {
		t.Fatalf("%d events; want one left", len(got))
	}
	if l, ok := got[0].(*user.SwarmMemberLeft); !ok || !l.NodeID.IsEqual(b) {
		t.Fatalf("got %v; want %v left", got[0], b)
	}
}

// A contract that is not a node contract never republishes the set.
func TestIndexedNonNodeContractPublishesNothing(t *testing.T) {
	e := newMemberEnv(t)
	e.mod.recordSwarm()
	e.query.found = []*auth.SignedContract{memberContract(t, e.issuer, astral.GenerateIdentity())}
	e.mod.onContractIndexed(&auth.SignedContract{Contract: &auth.Contract{Issuer: e.issuer, Subject: astral.GenerateIdentity()}})
	if got := e.events.take(); len(got) != 0 {
		t.Fatalf("a non-node contract emitted %v", got)
	}
}

// Before Run records the first set, nothing is published.
func TestPublishSwarmBeforeRecordEmitsNothing(t *testing.T) {
	e := newMemberEnv(t)
	e.query.found = []*auth.SignedContract{memberContract(t, e.issuer, astral.GenerateIdentity())}
	e.mod.publishSwarm()
	if got := e.events.take(); len(got) != 0 {
		t.Fatalf("publishing before Run emitted %v", got)
	}
}

// A member whose only contract expires leaves the published set at expiry:
// one SwarmMemberLeft for it and no other event.
func TestExpiryEmitsOneMemberLeft(t *testing.T) {
	t.Parallel()
	e := newMemberEnv(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	e.query.stage(memberContract(t, e.issuer, a), memberContractFor(t, e.issuer, b, 2*time.Second))
	e.mod.recordSwarm()

	time.Sleep(4 * time.Second)
	got := e.events.take()
	if len(got) != 1 {
		t.Fatalf("%d events %v; want one left", len(got), got)
	}
	if l, ok := got[0].(*user.SwarmMemberLeft); !ok || !l.NodeID.IsEqual(b) {
		t.Fatalf("got %v; want %v left", got[0], b)
	}
}

// A member with an overlapping later contract stays: the timer waits for the
// latest contract of each subject, so no republish runs at the earlier expiry.
func TestExpiryUsesLatestContractPerSubject(t *testing.T) {
	t.Parallel()
	e := newMemberEnv(t)
	b := astral.GenerateIdentity()
	e.query.stage(memberContractFor(t, e.issuer, b, 2*time.Second), memberContract(t, e.issuer, b))
	e.mod.recordSwarm()
	e.query.finds.Store(0)

	time.Sleep(3500 * time.Millisecond)
	if got := e.events.take(); len(got) != 0 {
		t.Fatalf("the earlier contract's expiry emitted %v", got)
	}
	if n := e.query.finds.Load(); n != 0 {
		t.Fatalf("a republish ran at the earlier expiry (%d auth queries)", n)
	}
}

// Republishing re-arms the one stored timer: many publishes before the
// deadline give one republish at it.
func TestRepublishKeepsOneTimer(t *testing.T) {
	t.Parallel()
	e := newMemberEnv(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	e.query.stage(memberContract(t, e.issuer, a), memberContractFor(t, e.issuer, b, 2*time.Second))
	e.mod.recordSwarm()
	for range 100 {
		e.mod.publishSwarm()
	}
	e.query.finds.Store(0)

	time.Sleep(4 * time.Second)
	// note: one republish reads the auth index twice: LocalSwarm and the re-arm.
	if n := e.query.finds.Load(); n != 2 {
		t.Fatalf("%d auth queries after the deadline; want 2 (one republish)", n)
	}
	if got := e.events.take(); len(got) != 1 {
		t.Fatalf("%d events %v; want one left", len(got), got)
	}
}

// The expiry of this node's own contract arms no republish.
func TestOwnExpiryArmsNothing(t *testing.T) {
	e := newMemberEnv(t)
	own := memberContractFor(t, e.issuer, e.mod.node.Identity(), 2*time.Second)
	member := memberContract(t, e.issuer, astral.GenerateIdentity())
	e.query.stage(own, member)
	if got, want := e.mod.nextMemberExpiry(), member.ExpiresAt.Time(); !got.Equal(want) {
		t.Fatalf("next expiry %v; want the member's %v", got, want)
	}
	e.query.stage(own)
	e.mod.recordSwarm()
	if e.mod.swarm.expiry != nil {
		t.Fatal("this node's own contract armed the expiry timer")
	}
}

// An unclaimed node arms no expiry timer.
func TestUnclaimedArmsNothing(t *testing.T) {
	q := &memberQuery{}
	issuer := astral.GenerateIdentity()
	q.stage(memberContractFor(t, issuer, astral.GenerateIdentity(), 2*time.Second))
	mod := &Module{db: testDB(t), node: &identityNode{id: astral.GenerateIdentity()}, now: time.Now}
	mod.Auth = &memberAuth{query: q}
	mod.Events = &eventRecorder{}
	mod.recordSwarm()
	if mod.swarm.expiry != nil {
		t.Fatal("an unclaimed node armed the expiry timer")
	}
}
