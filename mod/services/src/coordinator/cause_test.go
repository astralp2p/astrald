package coordinator

import (
	"testing"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// Spec: .ai/system/protocols/services/types/services.removed.md#cause

// expectRemoved reads the next removal and requires its cause.
func expectRemoved(t *testing.T, s *fakeSink, cause services.RemovalCause) *services.Removed {
	t.Helper()
	r := expectEvent(t, s, "removed").r
	if got := services.RemovalCause(r.Cause); got != cause {
		t.Fatalf("removed %d offerings with cause %v; want %v", len(r.Offerings), got, cause)
	}
	return r
}

// showMember makes member r contribute one shown offering per provider.
func showMember(t *testing.T, sink *fakeSink, r *Contribution, providers ...*astral.Identity) {
	t.Helper()
	for _, p := range providers {
		r.Update(remoteView(p, "player", true), true)
	}
	r.Initial(nil)
	for range providers {
		expectEvent(t, sink, "update")
	}
	expectEvent(t, sink, "boundary")
}

// A lost carry removes every offering the member contributed in one
// services.removed with cause member_lost.
func TestLostMemberRemovesWithMemberLost(t *testing.T) {
	e := newEnv(t)
	m := astral.GenerateIdentity()
	e.c.SetMembers([]*astral.Identity{m})
	carrier, sink := newCarrier(), newSink()
	st := e.followSwarm(sink, carrier, "player")
	defer st.Close()
	r := carrier.carry(m)
	showMember(t, sink, r, astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity())

	r.Lost()
	if rm := expectRemoved(t, sink, services.CauseMemberLost); len(rm.Offerings) != 3 {
		t.Fatalf("removed %d offerings; want 3", len(rm.Offerings))
	}
	expectNoEvent(t, sink)
	e.invariants()
}

// A member that leaves the swarm set has its offerings removed with cause
// member_left, and its carry is stopped once.
func TestLeftMemberRemovesWithMemberLeft(t *testing.T) {
	e := newEnv(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	e.c.SetMembers([]*astral.Identity{a, b})
	carrier, sink := newCarrier(), newSink()
	st := e.followSwarm(sink, carrier, "player")
	defer st.Close()
	carrier.carry(a).Initial(nil)
	rb := carrier.carry(b)
	pb := astral.GenerateIdentity()
	showMember(t, sink, rb, pb)

	e.c.SetMembers([]*astral.Identity{a})
	rm := expectRemoved(t, sink, services.CauseMemberLeft)
	if len(rm.Offerings) != 1 || !rm.Offerings[0].ProviderID.IsEqual(pb) {
		t.Fatalf("removed %+v; want B's offering", rm.Offerings)
	}
	expectNoEvent(t, sink)
	if n := carrier.stops(rb); n != 1 {
		t.Fatalf("B's carry stopped %d times; want 1", n)
	}
	e.invariants()
}

// followLocal shows one local offering to a follow and returns its key.
func followLocal(t *testing.T, e *env, sink *fakeSink) (*Stream, offeringKey) {
	t.Helper()
	src, tr := e.register("player")
	st, _ := e.discover(astral.GenerateIdentity(), true, sink, "player")
	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")
	return st, keyOf(src.Provider(), "player")
}

// retractTwice retracts k as hidden, then as member_lost, in one critical
// section.
func retractTwice(e *env, st *Stream, k offeringKey) {
	e.c.mu.Lock()
	defer e.c.mu.Unlock()
	st.retract(k, services.CauseHidden)
	st.retract(k, services.CauseMemberLost)
	wake(st.wake)
}

// A key retracted twice before the writer runs keeps its first cause.
func TestFirstRemovalCauseWins(t *testing.T) {
	e := newEnv(t)
	sink := newSink()
	st, k := followLocal(t, e, sink)
	defer st.Close()

	retractTwice(e, st, k)
	if rm := expectRemoved(t, sink, services.CauseHidden); len(rm.Offerings) != 1 {
		t.Fatalf("removed %d offerings; want 1", len(rm.Offerings))
	}
	expectNoEvent(t, sink)
}

// A key retracted twice while its view is being written keeps its first cause.
func TestFirstRemovalCauseWinsWhileInFlight(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	sink := newSink()
	sink.gate = make(chan struct{})
	st, _ := e.discover(astral.GenerateIdentity(), true, sink, "player")
	defer st.Close()
	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sink, "update") // the writer now waits in the gate

	retractTwice(e, st, keyOf(src.Provider(), "player"))
	close(sink.gate)
	expectRemoved(t, sink, services.CauseHidden)
	expectEvent(t, sink, "boundary")
	expectNoEvent(t, sink)
}

// A binding close and a private list change taken in one wake reach the
// consumer as two removals, binding_closed before hidden.
func TestRemovalsSplitByCause(t *testing.T) {
	e := newEnv(t)
	a, ta := e.register("player")
	b, tb := e.register("player")
	c, tc := e.register("player")
	sink := newSink()
	sink.gate = make(chan struct{})
	st := e.c.Discover(Request{Caller: astral.GenerateIdentity(), Names: []string{"player"}, Follow: true, Filter: filtered(true)})
	go st.Run(sink)
	defer st.Close()
	answer(t, a, expectAsk(t, ta), true)
	answer(t, b, expectAsk(t, tb), true)
	answer(t, c, expectAsk(t, tc), true)
	for _, kind := range []string{"update", "update", "update", "boundary"} {
		expectEvent(t, sink, kind)
		sink.gate <- struct{}{}
	}

	// why: the writer waits in the gate on c's view, so both retractions
	// below are pending at its next wake.
	if err := c.Change(&services.Change{All: true}); err != nil {
		t.Fatalf("change: %v", err)
	}
	answer(t, c, expectAsk(t, tc), true)
	expectEvent(t, sink, "update")
	b.Close()
	e.c.SetPrivate(&services.PrivateList{Entries: []*services.PrivateEntry{{Service: "player", ProviderID: a.Provider()}}})
	close(sink.gate)

	closed := expectRemoved(t, sink, services.CauseBindingClosed)
	hidden := expectRemoved(t, sink, services.CauseHidden)
	if len(closed.Offerings) != 1 || !closed.Offerings[0].ProviderID.IsEqual(b.Provider()) {
		t.Fatalf("binding_closed removed %+v; want b", closed.Offerings)
	}
	if len(hidden.Offerings) != 1 || !hidden.Offerings[0].ProviderID.IsEqual(a.Provider()) {
		t.Fatalf("hidden removed %+v; want a", hidden.Offerings)
	}
	expectNoEvent(t, sink)
}

// Every retraction call site writes its own cause.
func TestRemovalCausePerCallSite(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause services.RemovalCause
		local bool // the shown offering is local; otherwise the member's
		do    func(e *env, st *Stream, r *Contribution, src *Binding, p *astral.Identity)
	}{
		{"Contribution.Lost", services.CauseMemberLost, false, func(e *env, st *Stream, r *Contribution, src *Binding, p *astral.Identity) {
			r.Lost()
		}},
		{"dropMembers", services.CauseMemberLeft, false, func(e *env, st *Stream, r *Contribution, src *Binding, p *astral.Identity) {
			e.c.SetMembers(nil)
		}},
		{"Contribution.Remove", services.CauseMemberRemoved, false, func(e *env, st *Stream, r *Contribution, src *Binding, p *astral.Identity) {
			r.Remove([]*services.OfferingKey{{ProviderID: p, Name: "player"}})
		}},
		{"Contribution.Update hidden view", services.CauseHidden, false, func(e *env, st *Stream, r *Contribution, src *Binding, p *astral.Identity) {
			r.Update(remoteView(p, "player", true), false)
		}},
		{"Binding.Close", services.CauseBindingClosed, true, func(e *env, st *Stream, r *Contribution, src *Binding, p *astral.Identity) {
			src.Close()
		}},
		{"applyVisibilityChange", services.CauseHidden, true, func(e *env, st *Stream, r *Contribution, src *Binding, p *astral.Identity) {
			e.c.SetPrivate(&services.PrivateList{Entries: []*services.PrivateEntry{{Service: "player", ProviderID: p}}})
		}},
		{"offerFrom", services.CauseHidden, true, func(e *env, st *Stream, r *Contribution, src *Binding, p *astral.Identity) {
			// note: offerFrom hides a view the filter rejects when it is
			// answered; the binding turns private here without the private
			// list, so no other path retracts first.
			e.c.mu.Lock()
			defer e.c.mu.Unlock()
			src.open = false
			st.offerFrom(src, "player", keyOf(p, "player"), remoteView(p, "player", true))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			src, tr := e.register("player")
			m := astral.GenerateIdentity()
			e.c.SetMembers([]*astral.Identity{m})
			sink := newSink()
			st := e.c.Discover(Request{Caller: astral.GenerateIdentity(), Names: []string{"player"}, Follow: true, Swarm: true, Filter: filtered(true)})
			go st.Run(sink)
			defer st.Close()
			r := st.Contributions()[0]
			p := astral.GenerateIdentity()
			answer(t, src, expectAsk(t, tr), true)
			r.Update(remoteView(p, "player", true), true)
			r.Initial(nil)
			expectEvent(t, sink, "update")
			expectEvent(t, sink, "update")
			expectEvent(t, sink, "boundary")

			if tc.local {
				p = src.Provider()
			}
			tc.do(e, st, r, src, p)
			rm := expectRemoved(t, sink, tc.cause)
			if len(rm.Offerings) != 1 || !rm.Offerings[0].ProviderID.IsEqual(p) {
				t.Fatalf("removed %+v; want only %v", rm.Offerings, p)
			}
			expectNoEvent(t, sink)
		})
	}
}
