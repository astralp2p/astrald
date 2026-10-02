package coordinator

import (
	"testing"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// 3.2: one provider, one-shot.
func TestOneShotOneProvider(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	sink := newSink()
	_, done := e.discover(caller, false, sink, "player")

	a := expectAsk(t, tr)
	if a.Service != "player" || !a.CallerID.IsEqual(caller) || a.RequestID == 0 {
		t.Fatalf("ask %+v", a)
	}
	e.invariants()
	answer(t, src, a, true)

	u := expectEvent(t, sink, "update").u
	if !u.ProviderID.IsEqual(src.Provider()) || !bool(u.Available) {
		t.Fatalf("update %+v", u)
	}
	if b := expectEvent(t, sink, "boundary"); b.inc != nil {
		t.Fatalf("complete attempt reported incomplete: %+v", b.inc)
	}
	expectDone(t, done)
	e.invariants()
}

// 3.3: follow, then Change.
func TestFollowThenChange(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	sink := newSink()
	st, _ := e.discover(caller, true, sink, "player")
	defer st.Close()

	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")

	if err := src.Change(&services.Change{Callers: []*astral.Identity{caller}}); err != nil {
		t.Fatal(err)
	}
	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sink, "update")
	e.invariants()

	if err := src.Change(&services.Change{Callers: []*astral.Identity{astral.GenerateIdentity()}}); err != nil {
		t.Fatal(err)
	}
	expectNoAsk(t, tr)
}

// 3.4: no provider for a requested service ends at once with a bare eos.
func TestNothingToEvaluate(t *testing.T) {
	e := newEnv(t)
	sink := newSink()
	_, done := e.discover(astral.GenerateIdentity(), false, sink, "player")

	if b := expectEvent(t, sink, "boundary"); b.inc != nil {
		t.Fatalf("empty attempt reported incomplete: %+v", b.inc)
	}
	expectDone(t, done)
}

// 3.4 follow: a later registration reaches an empty follow.
func TestFollowSeesLaterRegistration(t *testing.T) {
	e := newEnv(t)
	sink := newSink()
	st, _ := e.discover(astral.GenerateIdentity(), true, sink, "player")
	defer st.Close()
	expectEvent(t, sink, "boundary")

	src, tr := e.register("player")
	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sink, "update")
	e.invariants()
}

// 4.1: one ask per caller at a time; a player-only follower causes no wallet ask.
func TestTwoServicesOneBinding(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player", "bitcoin-wallet")
	both, playerOnly := astral.GenerateIdentity(), astral.GenerateIdentity()
	sinkA, sinkB := newSink(), newSink()
	stA, _ := e.discover(both, true, sinkA, "player", "bitcoin-wallet")
	defer stA.Close()

	first := expectAsk(t, tr)
	expectNoAsk(t, tr) // the slot is busy
	answer(t, src, first, true)
	second := expectAsk(t, tr)
	if first.Service == second.Service {
		t.Fatalf("asked %s twice", first.Service)
	}
	answer(t, src, second, true)
	expectEvent(t, sinkA, "update")
	expectEvent(t, sinkA, "update")
	expectEvent(t, sinkA, "boundary")

	stB, _ := e.discover(playerOnly, true, sinkB, "player")
	defer stB.Close()
	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sinkB, "update")
	expectEvent(t, sinkB, "boundary")

	if err := src.Change(&services.Change{All: true}); err != nil {
		t.Fatal(err)
	}
	asked := map[string]int{}
	for i := 0; i < 3; i++ {
		a := expectAsk(t, tr)
		asked[a.CallerID.String()+"/"+string(a.Service)]++
		answer(t, src, a, true)
	}
	expectNoAsk(t, tr)
	if asked[playerOnly.String()+"/bitcoin-wallet"] != 0 {
		t.Fatal("a player-only follower caused a wallet ask")
	}
	e.invariants()
}

// 4.2: a Change during an ask is retained; the answer counts for the initial
// attempt, then the slot asks again.
func TestChangeDuringAsk(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	sink := newSink()
	st, _ := e.discover(caller, true, sink, "player")
	defer st.Close()

	a := expectAsk(t, tr)
	if err := src.Change(&services.Change{Callers: []*astral.Identity{caller}}); err != nil {
		t.Fatal(err)
	}
	expectNoAsk(t, tr)
	answer(t, src, a, true)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")

	answer(t, src, expectAsk(t, tr), false) // refresh withdraws
	if u := expectEvent(t, sink, "update").u; bool(u.Available) {
		t.Fatal("withdrawal arrived as available")
	}
	e.invariants()
}
