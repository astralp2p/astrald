package user

import (
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

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
	c, err := user.NewMembership(issuer, subject, user.TierMember, nil, validity)
	if err != nil {
		t.Fatal(err)
	}
	return &auth.SignedContract{Contract: c, IssuerSig: viewSig(), SubjectSig: viewSig()}
}

// memberEnv is a claimed node over a real store, whose memberships issuer signs.
type memberEnv struct {
	mod    *Module
	events *eventRecorder
	issuer *astral.Identity
}

func newMemberEnv(t *testing.T) *memberEnv {
	issuer := astral.GenerateIdentity()
	mod := newStoreModule(t, astral.GenerateIdentity(), issuer, nil, time.Now, realRules)
	e := &memberEnv{mod: mod, events: &eventRecorder{}, issuer: issuer}
	mod.Events = e.events
	mod.Nodes = newLinkNodes()
	t.Cleanup(e.stopExpiry)
	return e
}

func (e *memberEnv) stopExpiry() {
	e.mod.swarm.mu.Lock()
	defer e.mod.swarm.mu.Unlock()
	e.mod.stopExpiry()
}

// add stores a membership of subject valid for validity and returns it.
func (e *memberEnv) add(t *testing.T, subject *astral.Identity, validity time.Duration) *auth.SignedContract {
	t.Helper()
	sc := memberContractFor(t, e.issuer, subject, validity)
	mergeEntry(t, e.mod, sc)
	return sc
}

// One event per membership change, through the indexing hook and through an
// expulsion; none when the member set is unchanged.
func TestPublishSwarmEmitsOneEventPerChange(t *testing.T) {
	e := newMemberEnv(t)
	a := astral.GenerateIdentity()
	e.add(t, a, time.Hour)
	e.mod.recordSwarm()
	if got := e.events.take(); len(got) != 0 {
		t.Fatalf("recording the first set emitted %v", got)
	}

	b := astral.GenerateIdentity()
	cb := e.add(t, b, time.Hour)
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

	banNode(t, e.mod, e.issuer, b)
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
	e.add(t, astral.GenerateIdentity(), time.Hour)
	e.mod.onContractIndexed(&auth.SignedContract{Contract: &auth.Contract{Issuer: e.issuer, Subject: astral.GenerateIdentity()}})
	if got := e.events.take(); len(got) != 0 {
		t.Fatalf("a non-node contract emitted %v", got)
	}
}

// Before Run records the first set, nothing is published.
func TestPublishSwarmBeforeRecordEmitsNothing(t *testing.T) {
	e := newMemberEnv(t)
	e.add(t, astral.GenerateIdentity(), time.Hour)
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
	e.add(t, a, time.Hour)
	e.add(t, b, 2*time.Second)
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
	e.add(t, b, 2*time.Second)
	e.add(t, b, time.Hour)
	e.mod.recordSwarm()
	e.mod.swarm.mu.Lock()
	armed := e.mod.swarm.expiry
	e.mod.swarm.mu.Unlock()
	if armed == nil {
		t.Fatal("recording a set with expiring members armed no timer")
	}

	time.Sleep(3500 * time.Millisecond)
	if got := e.events.take(); len(got) != 0 {
		t.Fatalf("the earlier contract's expiry emitted %v", got)
	}
	// note: every republish re-arms the one stored timer, so the same timer means none ran.
	e.mod.swarm.mu.Lock()
	defer e.mod.swarm.mu.Unlock()
	if e.mod.swarm.expiry != armed {
		t.Fatal("a republish ran at the earlier expiry")
	}
}

// Republishing re-arms the one stored timer: many publishes before the
// deadline give one republish at it.
func TestRepublishKeepsOneTimer(t *testing.T) {
	t.Parallel()
	e := newMemberEnv(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	e.add(t, a, time.Hour)
	e.add(t, b, 2*time.Second)
	e.mod.recordSwarm()
	for range 100 {
		e.mod.publishSwarm()
	}

	time.Sleep(4 * time.Second)
	// note: the members come from SwarmState, so the one republish shows as
	// its one event alone.
	if got := e.events.take(); len(got) != 1 {
		t.Fatalf("%d events %v; want one left", len(got), got)
	}
}

// The expiry of this node's own contract arms no republish: a member's does.
func TestOwnExpiryArmsNothing(t *testing.T) {
	e := newMemberEnv(t)
	e.add(t, e.mod.node.Identity(), 2*time.Second)
	member := e.add(t, astral.GenerateIdentity(), time.Hour)
	if got, want := e.mod.nextMemberExpiry(), member.ExpiresAt.Time(); !got.Equal(want) {
		t.Fatalf("next expiry %v; want the member's %v", got, want)
	}

	// note: a store never forgets a membership, so the swarm of this node alone is built apart.
	own := newMemberEnv(t)
	own.add(t, own.mod.node.Identity(), 2*time.Second)
	own.mod.recordSwarm()
	if own.mod.swarm.expiry != nil {
		t.Fatal("this node's own contract armed the expiry timer")
	}
}

// An unclaimed node arms no expiry timer.
func TestUnclaimedArmsNothing(t *testing.T) {
	node := astral.GenerateIdentity()
	mod := newStoreModule(t, node, nil, nil, time.Now, realRules)
	mod.Events = &eventRecorder{}
	storeMembership(t, mod, node, astral.GenerateIdentity(), user.TierMember, time.Now().Add(2*time.Second))
	mod.recordSwarm()
	if mod.swarm.expiry != nil {
		t.Fatal("an unclaimed node armed the expiry timer")
	}
}
