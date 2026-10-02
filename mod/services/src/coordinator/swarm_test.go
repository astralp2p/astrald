package coordinator

import (
	"testing"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

func remoteView(provider *astral.Identity, name string, available bool) *services.Update {
	return &services.Update{Available: astral.Bool(available), Name: astral.String8(name), ProviderID: provider}
}

// A swarm discovery waits for the local provider and every member, then ends
// complete.
func TestSwarmOneShotWaitsForEveryMember(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	m1, m2 := astral.GenerateIdentity(), astral.GenerateIdentity()
	sink := newSink()
	st, done := e.discoverSwarm(astral.GenerateIdentity(), false, sink, []*astral.Identity{m1, m2}, "player")
	r1, r2 := st.Remotes()[0], st.Remotes()[1]
	if !r1.Member().IsEqual(m1) || !r2.Member().IsEqual(m2) {
		t.Fatal("remotes are not in request order")
	}
	e.invariants()

	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sink, "update")
	p1 := astral.GenerateIdentity()
	r1.Offer(remoteView(p1, "player", true))
	r1.Initial(nil)
	if u := expectEvent(t, sink, "update").u; !u.ProviderID.IsEqual(p1) {
		t.Fatalf("got %v; want the member's provider", u.ProviderID)
	}
	expectNoEvent(t, sink)

	r2.Initial(nil)
	if b := expectEvent(t, sink, "boundary"); b.inc != nil {
		t.Fatalf("incomplete %+v; want complete", b.inc)
	}
	expectDone(t, done)
	e.invariants()
}

// A member that reports incomplete, or is lost, makes the host's outcome
// incomplete for those services; the others still resolve.
func TestSwarmMemberFailuresAreIncomplete(t *testing.T) {
	e := newEnv(t)
	sink := newSink()
	st, _ := e.discoverSwarm(astral.GenerateIdentity(), true, sink, []*astral.Identity{astral.GenerateIdentity(), astral.GenerateIdentity()}, "player", "wallet")
	defer st.Close()

	st.Remotes()[0].Initial([]string{"wallet"})
	st.Remotes()[1].Lost()
	b := expectEvent(t, sink, "boundary")
	if b.inc == nil || len(b.inc.Services) != 2 {
		t.Fatalf("incomplete %+v; want player and wallet", b.inc)
	}
	e.invariants()
}

// A member that never answers fails at the budget, like a silent provider.
func TestSwarmSilentMemberFailsAtTheBudget(t *testing.T) {
	e := newEnv(t)
	sink := newSink()
	st, done := e.discoverSwarm(astral.GenerateIdentity(), false, sink, []*astral.Identity{astral.GenerateIdentity()}, "player")
	_ = st
	expectNoEvent(t, sink)
	e.clock.Advance(budget)
	if b := expectEvent(t, sink, "boundary"); b.inc == nil || b.inc.Services[0] != "player" {
		t.Fatalf("incomplete %+v; want player", b.inc)
	}
	expectDone(t, done)
}

// Losing a member retracts exactly the offerings it contributed and the
// consumer saw; local offerings stay. The member can contribute again.
func TestSwarmLossRetractsTheMembersOfferings(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	sink := newSink()
	st, _ := e.discoverSwarm(astral.GenerateIdentity(), true, sink, []*astral.Identity{astral.GenerateIdentity()}, "player")
	defer st.Close()
	r := st.Remotes()[0]

	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sink, "update")
	shown, hidden := astral.GenerateIdentity(), astral.GenerateIdentity()
	r.Offer(remoteView(shown, "player", true))
	r.Offer(remoteView(hidden, "player", false)) // never introduced: suppressed
	r.Initial(nil)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")

	r.Lost()
	rm := expectEvent(t, sink, "removed").r
	if len(rm.Offerings) != 1 || !rm.Offerings[0].ProviderID.IsEqual(shown) {
		t.Fatalf("removed %+v; want only the member's shown offering", rm.Offerings)
	}

	r.Offer(remoteView(shown, "player", true))
	if u := expectEvent(t, sink, "update").u; !u.ProviderID.IsEqual(shown) {
		t.Fatal("a restored member's offering did not reach the follow")
	}
	e.invariants()
}

// A member's removal retracts only keys the member contributed, and views of
// services nobody requested are ignored.
func TestSwarmRemoveAndUnrequestedViews(t *testing.T) {
	e := newEnv(t)
	sink := newSink()
	st, _ := e.discoverSwarm(astral.GenerateIdentity(), true, sink, []*astral.Identity{astral.GenerateIdentity(), astral.GenerateIdentity()}, "player")
	defer st.Close()
	r1, r2 := st.Remotes()[0], st.Remotes()[1]
	p := astral.GenerateIdentity()

	r1.Offer(remoteView(p, "player", true))
	r1.Offer(remoteView(p, "wallet", true))
	r1.Initial(nil)
	r2.Initial(nil)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")
	expectNoEvent(t, sink)

	r2.Remove([]*services.OfferingKey{{ProviderID: p, Name: "player"}})
	expectNoEvent(t, sink)
	r1.Remove([]*services.OfferingKey{{ProviderID: p, Name: "player"}})
	expectEvent(t, sink, "removed")
}

// Closing the consumer stream closes Done, which ends the member readers.
func TestSwarmStreamCloseSignalsDone(t *testing.T) {
	e := newEnv(t)
	st, _ := e.discoverSwarm(astral.GenerateIdentity(), true, newSink(), []*astral.Identity{astral.GenerateIdentity()}, "player")
	st.Close()
	select {
	case <-st.Done():
	default:
		t.Fatal("Done is open after Close")
	}
	st.Remotes()[0].Offer(remoteView(astral.GenerateIdentity(), "player", true))
	st.Remotes()[0].Lost()
	e.invariants()
}
