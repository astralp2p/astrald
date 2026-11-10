package coordinator

import (
	"testing"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// A tagged follow re-sends a shown offering with its new tag when the private
// list covers it, without waiting for the provider: a provider that does not
// answer the re-evaluation must not stay tagged open.
func TestTaggedFollowRetagsWithoutTheProvider(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	sink := newSink()
	st := e.c.Discover(Request{Caller: astral.GenerateIdentity(), Names: []string{"player"}, Follow: true, Tagged: true})
	go st.Run(sink)
	answer(t, src, expectAsk(t, tr), true)
	if ev := expectEvent(t, sink, "update"); !ev.tagged || !ev.open {
		t.Fatalf("got %+v; want the player tagged open", ev)
	}
	expectEvent(t, sink, "boundary")

	e.c.SetPrivate(&services.PrivateList{Entries: []*services.PrivateEntry{{Service: "player"}}})
	if ev := expectEvent(t, sink, "update"); !ev.tagged || ev.open {
		t.Fatalf("got %+v; want the player re-sent tagged private", ev)
	}

	e.c.SetPrivate(nil)
	if ev := expectEvent(t, sink, "update"); !ev.open {
		t.Fatalf("got %+v; want the player re-sent tagged open", ev)
	}
}

// A view queued for a one-shot stream before the private list hides its
// provider is never written to a caller that may not see it.
func TestQueuedViewIsWithheldWhenItsProviderTurnsPrivate(t *testing.T) {
	e := newEnv(t)
	a, ta := e.register("player")
	b, tb := e.register("player")
	sink := newSink()
	sink.gate = make(chan struct{})
	st := e.c.Discover(Request{Caller: astral.GenerateIdentity(), Names: []string{"player"}, Filter: &Visibility{SwarmCaller: true, Permitted: map[string]bool{}}})
	go st.Run(sink)
	answer(t, a, expectAsk(t, ta), true)
	expectEvent(t, sink, "update") // the writer now waits in the gate
	answer(t, b, expectAsk(t, tb), true)
	e.c.SetPrivate(&services.PrivateList{Entries: []*services.PrivateEntry{{Service: "player", ProviderID: b.Provider()}}})
	sink.gate <- struct{}{}
	ev := <-sink.events
	sink.gate <- struct{}{}
	if ev.kind == "update" && ev.u.ProviderID.IsEqual(b.Provider()) {
		t.Fatal("the private provider's queued view was written")
	}
}
