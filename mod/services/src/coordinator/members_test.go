package coordinator

import (
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// fakeCarrier records every carry it starts and whether it was stopped.
type fakeCarrier struct {
	mu      sync.Mutex
	carried []*Contribution
	stopped map[*Contribution]bool
}

func newCarrier() *fakeCarrier { return &fakeCarrier{stopped: map[*Contribution]bool{}} }

func (f *fakeCarrier) Carry(r *Contribution) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.carried = append(f.carried, r)
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.stopped[r] = true
	}
}

// carry returns the contribution carried to member, or nil.
func (f *fakeCarrier) carry(member *astral.Identity) *Contribution {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.carried {
		if r.Member().IsEqual(member) {
			return r
		}
	}
	return nil
}

func (f *fakeCarrier) isStopped(r *Contribution) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopped[r]
}

func (e *env) followSwarm(sink *fakeSink, carrier Carrier, names ...string) *Stream {
	st := e.c.Discover(Request{Caller: astral.GenerateIdentity(), Names: names, Follow: true, Swarm: true, Carrier: carrier})
	go st.Run(sink)
	return st
}

// A member that joins an open follow is carried to, and its offering reaches
// the follow without a reopen.
func TestSwarmMemberJoinsAnOpenFollow(t *testing.T) {
	e := newEnv(t)
	a := astral.GenerateIdentity()
	e.c.SetMembers([]*astral.Identity{a})
	carrier, sink := newCarrier(), newSink()
	st := e.followSwarm(sink, carrier, "player")
	defer st.Close()
	carrier.carry(a).Initial(nil)
	expectEvent(t, sink, "boundary")

	b := astral.GenerateIdentity()
	e.c.SetMembers([]*astral.Identity{a, b})
	rb := carrier.carry(b)
	if rb == nil {
		t.Fatal("the joined member is not carried to")
	}
	p := astral.GenerateIdentity()
	rb.Update(remoteView(p, "player", true), true)
	if u := expectEvent(t, sink, "update").u; !u.ProviderID.IsEqual(p) {
		t.Fatal("the joined member's offering did not reach the follow")
	}
	if len(carrier.carried) != 2 {
		t.Fatalf("%d carries; want one per member", len(carrier.carried))
	}
	e.invariants()
}

// A member that leaves an open follow has its shown offerings removed and its
// carry stopped; later views from it are ignored.
func TestSwarmMemberLeavesAnOpenFollow(t *testing.T) {
	e := newEnv(t)
	b := astral.GenerateIdentity()
	e.c.SetMembers([]*astral.Identity{b})
	carrier, sink := newCarrier(), newSink()
	st := e.followSwarm(sink, carrier, "player")
	defer st.Close()
	rb := carrier.carry(b)
	p := astral.GenerateIdentity()
	rb.Update(remoteView(p, "player", true), true)
	rb.Initial(nil)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")

	e.c.SetMembers(nil)
	rm := expectEvent(t, sink, "removed").r
	if len(rm.Offerings) != 1 || !rm.Offerings[0].ProviderID.IsEqual(p) {
		t.Fatalf("removed %+v; want the departed member's offering", rm.Offerings)
	}
	if !carrier.isStopped(rb) {
		t.Fatal("the departed member's carry was not stopped")
	}
	rb.Update(remoteView(p, "player", true), true)
	expectNoEvent(t, sink)
	if len(st.Contributions()) != 0 {
		t.Fatal("the departed member still contributes")
	}
	e.invariants()
}

// A member that joins before the initial attempt ends adds no initial work.
func TestSwarmJoinBeforeBoundaryAddsNoInitialWork(t *testing.T) {
	e := newEnv(t)
	a := astral.GenerateIdentity()
	e.c.SetMembers([]*astral.Identity{a})
	carrier, sink := newCarrier(), newSink()
	st := e.followSwarm(sink, carrier, "player")
	defer st.Close()

	e.c.SetMembers([]*astral.Identity{a, astral.GenerateIdentity()})
	carrier.carry(a).Initial(nil)
	if b := expectEvent(t, sink, "boundary"); b.inc != nil {
		t.Fatalf("incomplete %+v; want complete", b.inc)
	}
	e.invariants()
}

// Closing a follow stops every carry it started, including one added after it
// opened.
func TestSwarmCloseStopsEveryCarry(t *testing.T) {
	e := newEnv(t)
	e.c.SetMembers([]*astral.Identity{astral.GenerateIdentity()})
	carrier := newCarrier()
	st := e.followSwarm(newSink(), carrier, "player")
	e.c.SetMembers(append(append([]*astral.Identity(nil), e.c.members...), astral.GenerateIdentity()))

	st.Close()
	if len(carrier.carried) != 2 {
		t.Fatalf("%d carries; want 2", len(carrier.carried))
	}
	for _, r := range carrier.carried {
		if !carrier.isStopped(r) {
			t.Fatalf("carry to %v not stopped", r.Member())
		}
	}
}

// A one-shot or local discovery never gains or loses members.
func TestSwarmMembershipLeavesOtherStreams(t *testing.T) {
	e := newEnv(t)
	carrier := newCarrier()
	oneShot := e.c.Discover(Request{Caller: astral.GenerateIdentity(), Names: []string{"player"}, Swarm: true, Carrier: carrier})
	local := e.c.Discover(Request{Caller: astral.GenerateIdentity(), Names: []string{"player"}, Follow: true, Carrier: carrier})
	defer oneShot.Close()
	defer local.Close()

	e.c.SetMembers([]*astral.Identity{astral.GenerateIdentity()})
	if len(carrier.carried) != 0 || len(oneShot.Contributions()) != 0 || len(local.Contributions()) != 0 {
		t.Fatal("a member joined a stream that is not a swarm follow")
	}
}
