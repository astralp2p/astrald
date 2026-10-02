package coordinator

import (
	"testing"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// 4.6: a slow consumer receives the latest view, not every intermediate one.
func TestSlowConsumerGetsLatestView(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	sink := newSink()
	sink.gate = make(chan struct{})
	st, _ := e.discover(caller, true, sink, "player")
	defer st.Close()

	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sink, "update") // blocked in flight
	for i := 0; i < 3; i++ {
		if err := src.Change(&services.Change{Callers: []*astral.Identity{caller}}); err != nil {
			t.Fatal(err)
		}
		a := expectAsk(t, tr)
		err := src.Answer(&services.Answer{RequestID: a.RequestID, Update: &services.Update{Available: true, Name: "player", Info: info(i)}})
		if err != nil {
			t.Fatal(err)
		}
	}
	sink.gate <- struct{}{}

	got := []string{}
	for i := 0; i < 2; i++ {
		ev := <-sink.events
		got = append(got, ev.kind)
		if ev.kind == "update" && len(ev.u.Info.Objects()) != 3 {
			t.Fatalf("got intermediate view with %d objects; want the latest", len(ev.u.Info.Objects()))
		}
		sink.gate <- struct{}{}
	}
	if got[0] != "boundary" || got[1] != "update" {
		t.Fatalf("order %v; want boundary before the refresh view", got)
	}
	expectNoEvent(t, sink)
}

func info(n int) *astral.Bundle {
	b := astral.NewBundle()
	for i := 0; i <= n; i++ {
		_ = b.Append(astral.NewString8(string(rune('a' + i))))
	}
	return b
}

// A provider that offers nothing to a stream that never saw it sends nothing;
// once shown, an unavailable answer withdraws it.
func TestNegativeSuppression(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	sink := newSink()
	st, _ := e.discover(caller, true, sink, "player")
	defer st.Close()

	answer(t, src, expectAsk(t, tr), false)
	if b := expectEvent(t, sink, "boundary"); b.inc != nil {
		t.Fatal("an explicit unavailable answer is complete work")
	}
	expectNoEvent(t, sink)

	change := &services.Change{Callers: []*astral.Identity{caller}}
	_ = src.Change(change)
	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sink, "update")
	_ = src.Change(change)
	answer(t, src, expectAsk(t, tr), false)
	if u := expectEvent(t, sink, "update").u; bool(u.Available) {
		t.Fatal("want withdrawal")
	}
}

// 4.7: closing a quiet follow ends its writer and leaves no refresh demand.
func TestConsumerClosesQuietFollow(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	sink := newSink()
	st, done := e.discover(caller, true, sink, "player")

	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")

	st.Close()
	expectDone(t, done)
	_ = src.Change(&services.Change{Callers: []*astral.Identity{caller}})
	expectNoAsk(t, tr)
	e.invariants()
}

// A one-shot stream does not receive refreshes triggered by another stream of
// the same caller.
func TestOneShotIgnoresOtherStreamsRefresh(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	followSink, oneSink := newSink(), newSink()
	follow, _ := e.discover(caller, true, followSink, "player")
	defer follow.Close()
	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, followSink, "update")
	expectEvent(t, followSink, "boundary")

	_, done := e.discover(caller, false, oneSink, "player")
	own := expectAsk(t, tr)
	answer(t, src, own, true)
	expectEvent(t, oneSink, "update")
	expectEvent(t, oneSink, "boundary")
	expectDone(t, done)
	expectEvent(t, followSink, "update")
}

// A new discovery never consumes an answer to an ask issued before it.
func TestNewDiscoveryGetsAFreshAsk(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	first, second := newSink(), newSink()
	stA, _ := e.discover(caller, true, first, "player")
	defer stA.Close()
	old := expectAsk(t, tr)

	stB, _ := e.discover(caller, true, second, "player")
	defer stB.Close()
	answer(t, src, old, true)
	expectEvent(t, first, "update")
	expectEvent(t, first, "boundary")
	expectEvent(t, second, "update") // a follower still sees the refresh
	expectNoEvent(t, second)         // but its own attempt is not finished

	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, second, "update") // its own fresh view is owed
	expectEvent(t, second, "boundary")
	e.invariants()
}
