package coordinator

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// nextEvent returns the next event of any kind.
func nextEvent(t *testing.T, sink *fakeSink) event {
	t.Helper()
	select {
	case ev := <-sink.events:
		return ev
	case <-time.After(waitFor):
		t.Fatal("no event arrived")
		return event{}
	}
}

// readUntilBoundary reads events up to the boundary and returns those before it.
func readUntilBoundary(t *testing.T, sink *fakeSink) []event {
	t.Helper()
	var list []event
	for {
		ev := nextEvent(t, sink)
		if ev.kind == "boundary" {
			return list
		}
		list = append(list, ev)
	}
}

// A revoke while a view waits unsent drops that view; a view already handed
// to the writer is removed with cause hidden once written.
func TestSetFilterDropsAQueuedView(t *testing.T) {
	e := newEnv(t)
	a, ta := e.register("contacts")
	b, tb := e.register("contacts")
	sink := newSink()
	sink.gate = make(chan struct{})
	st := e.c.Discover(Request{Caller: astral.GenerateIdentity(), Names: []string{"contacts"}, Follow: true, Filter: filtered(true)})
	defer st.Close()
	go st.Run(sink)
	answer(t, a, expectAsk(t, ta), true)
	expectEvent(t, sink, "update") // the writer now waits in the gate
	answer(t, b, expectAsk(t, tb), true)

	e.c.SetFilter(st, filtered(false), nil)
	close(sink.gate)

	removed := false
	for _, ev := range readUntilBoundary(t, sink) {
		switch {
		case ev.kind == "update" && ev.u.ProviderID.IsEqual(b.Provider()):
			t.Fatal("the queued view of a provider the caller may no longer see was written")
		case ev.kind == "removed":
			if len(ev.r.Offerings) != 1 || !ev.r.Offerings[0].ProviderID.IsEqual(a.Provider()) {
				t.Fatalf("removed %+v; want the written provider only", ev.r.Offerings)
			}
			if services.RemovalCause(ev.r.Cause) != services.CauseHidden {
				t.Fatalf("cause %v; want hidden", ev.r.Cause)
			}
			removed = true
		}
	}
	if !removed {
		t.Fatal("the written view was not removed")
	}
	e.invariants()
}

// A filter that newly allows a private binding evaluates it again for the
// follow; the changed not_granted list precedes the view.
func TestSetFilterRevealsABindingToAFollow(t *testing.T) {
	e := newEnv(t)
	p, tp := e.registerPrivate("contacts")
	sink := newSink()
	st := e.c.Discover(Request{Caller: astral.GenerateIdentity(), Names: []string{"contacts"}, Follow: true, Filter: filtered(true)})
	defer st.Close()
	go st.Run(sink)
	answer(t, p, expectAsk(t, tp), true)
	expectEvent(t, sink, "boundary")
	expectNoEvent(t, sink)

	e.c.SetFilter(st, filtered(true, "contacts"), &services.NotGranted{})
	if ng := expectEvent(t, sink, "not_granted").ng; len(ng.Services) != 0 {
		t.Fatalf("not granted %v; want an empty list", ng.Services)
	}
	answer(t, p, expectAsk(t, tp), true)
	if u := expectEvent(t, sink, "update").u; !u.ProviderID.IsEqual(p.Provider()) {
		t.Fatalf("got %v; want the private provider after the grant", u.ProviderID)
	}
	e.invariants()
}

// A filter that changes nothing for a follow asks no provider and writes
// nothing.
func TestSetFilterWithoutChangeIsQuiet(t *testing.T) {
	e := newEnv(t)
	p, tp := e.register("contacts")
	sink := newSink()
	st := e.c.Discover(Request{Caller: astral.GenerateIdentity(), Names: []string{"contacts"}, Follow: true, Filter: filtered(true)})
	defer st.Close()
	go st.Run(sink)
	answer(t, p, expectAsk(t, tp), true)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")

	e.c.SetFilter(st, filtered(true), nil)
	expectNoAsk(t, tp)
	expectNoEvent(t, sink)
}

// A member offering the caller may no longer see is removed with cause
// hidden, and Offered reports the tag each offering arrived with.
func TestContributionHideRemovesWithCauseHidden(t *testing.T) {
	e := newEnv(t)
	m := astral.GenerateIdentity()
	e.c.SetMembers([]*astral.Identity{m})
	carrier, sink := newCarrier(), newSink()
	st := e.followSwarm(sink, carrier, "contacts")
	defer st.Close()
	r := carrier.carry(m)
	private, open := astral.GenerateIdentity(), astral.GenerateIdentity()
	r.Update(remoteView(private, "contacts", true), false, true)
	r.Update(remoteView(open, "contacts", true), true, true)
	r.Initial(nil)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")

	var hide []Offering
	for _, o := range r.Offered() {
		if o.ProviderID.IsEqual(private) {
			if o.Open {
				t.Fatal("the private offering is reported open")
			}
			hide = append(hide, o)
		}
	}
	if len(hide) != 1 {
		t.Fatalf("offered %+v; want the private offering among them", r.Offered())
	}
	r.Hide(hide)
	rm := expectEvent(t, sink, "removed").r
	if len(rm.Offerings) != 1 || !rm.Offerings[0].ProviderID.IsEqual(private) || services.RemovalCause(rm.Cause) != services.CauseHidden {
		t.Fatalf("removed %+v cause %v; want the private offering, hidden", rm.Offerings, rm.Cause)
	}
	if left := r.Offered(); len(left) != 1 || !left[0].ProviderID.IsEqual(open) {
		t.Fatalf("offered after hide %+v; want the open offering only", left)
	}
	e.invariants()
}

// A restarted contribution stops its carry and starts a new one without
// retracting anything; an offering the member sends again stays shown, and
// one it does not is removed at its next initial outcome.
func TestRestartContributionsKeepsOfferingsSentAgain(t *testing.T) {
	e := newEnv(t)
	m := astral.GenerateIdentity()
	e.c.SetMembers([]*astral.Identity{m})
	carrier, sink := newCarrier(), newSink()
	st := e.followSwarm(sink, carrier, "player")
	defer st.Close()
	r := carrier.carry(m)
	kept, dropped := astral.GenerateIdentity(), astral.GenerateIdentity()
	r.Update(remoteView(kept, "player", true), true, true)
	r.Update(remoteView(dropped, "player", true), true, true)
	r.Initial(nil)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")

	e.c.RestartContributions(st)
	if n := carrier.stops(r); n != 1 {
		t.Fatalf("the carry was stopped %d times; want once", n)
	}
	if n := len(carrier.carried); n != 2 {
		t.Fatalf("%d carries started; want the first and the restart", n)
	}
	expectNoEvent(t, sink)

	r.Update(remoteView(kept, "player", true), true, true)
	r.Initial(nil)
	var removed []*services.OfferingKey
	for range 2 {
		ev := nextEvent(t, sink)
		if ev.kind == "removed" {
			removed = append(removed, ev.r.Offerings...)
		}
	}
	if len(removed) != 1 || !removed[0].ProviderID.IsEqual(dropped) {
		t.Fatalf("removed %+v; want only the offering the member did not send again", removed)
	}
	expectNoEvent(t, sink)
	e.invariants()
}
