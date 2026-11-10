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

// registerPrivate binds a private provider of names.
func (e *env) registerPrivate(names ...string) (*Binding, *fakeTransport) {
	e.t.Helper()
	tr := newTransport()
	b, err := e.c.Register(astral.GenerateIdentity(), names, false, tr)
	if err != nil {
		e.t.Fatalf("register %v: %v", names, err)
	}
	return b, tr
}

func filtered(swarm bool, permitted ...string) *Visibility {
	v := &Visibility{SwarmCaller: swarm, Permitted: map[string]bool{}}
	for _, s := range permitted {
		v.Permitted[s] = true
	}
	return v
}

// The filter passes an open offering to a swarm caller and a private one only
// with a permit; a filtered answer still ends its initial evaluation.
func TestFilterDecidesPerBinding(t *testing.T) {
	for _, tc := range []struct {
		name          string
		filter        *Visibility
		open, private bool
	}{
		{"swarm caller", filtered(true), true, false},
		{"swarm caller with a permit", filtered(true, "player"), true, true},
		{"other caller", filtered(false), false, false},
		{"other caller with a permit", filtered(false, "player"), true, true},
		{"unfiltered", nil, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			open, to := e.register("player")
			private, tp := e.registerPrivate("player")
			sink := newSink()
			st := e.c.Discover(Request{Caller: astral.GenerateIdentity(), Names: []string{"player"}, Filter: tc.filter})
			go st.Run(sink)
			answer(t, open, expectAsk(t, to), true)
			answer(t, private, expectAsk(t, tp), true)
			seen := map[string]bool{}
			for {
				ev := <-sink.events
				if ev.kind == "boundary" {
					if ev.inc != nil {
						t.Fatalf("incomplete %v; a filtered answer is complete work", ev.inc.Services)
					}
					break
				}
				seen[ev.u.ProviderID.String()] = true
			}
			if seen[open.Provider().String()] != tc.open || seen[private.Provider().String()] != tc.private {
				t.Fatalf("saw open %v, private %v; want %v, %v", seen[open.Provider().String()], seen[private.Provider().String()], tc.open, tc.private)
			}
		})
	}
}

// Names lists every advertised service once, sorted.
func TestNamesListsAdvertisedServices(t *testing.T) {
	e := newEnv(t)
	e.register("radio", "player")
	e.registerPrivate("player")
	if got := e.c.Names(); len(got) != 2 || got[0] != "player" || got[1] != "radio" {
		t.Fatalf("names %v; want player, radio", got)
	}
}

// A filtered follow that may newly see an offering after the list changes
// re-evaluates it; an unfiltered, untagged follow is left alone.
func TestListRemovalReevaluatesForAFilteredFollow(t *testing.T) {
	e := newEnv(t)
	b, tr := e.register("player")
	e.c.SetPrivate(&services.PrivateList{Entries: []*services.PrivateEntry{{Service: "player"}}})
	sink := newSink()
	st := e.c.Discover(Request{Caller: astral.GenerateIdentity(), Names: []string{"player"}, Follow: true, Filter: filtered(true)})
	go st.Run(sink)
	answer(t, b, expectAsk(t, tr), true)
	expectEvent(t, sink, "boundary")

	plain := newSink()
	other := e.c.Discover(Request{Caller: astral.GenerateIdentity(), Names: []string{"player"}, Follow: true})
	go other.Run(plain)
	answer(t, b, expectAsk(t, tr), true)
	expectEvent(t, plain, "update")
	expectEvent(t, plain, "boundary")

	e.c.SetPrivate(nil)
	answer(t, b, expectAsk(t, tr), true)
	if ev := expectEvent(t, sink, "update"); !ev.u.ProviderID.IsEqual(b.Provider()) {
		t.Fatalf("got %v; want the player", ev.u.ProviderID)
	}
	expectNoEvent(t, plain)
}

// A member view the caller may not see removes the same offering shown
// before, and is dropped when it was never shown.
func TestHiddenMemberViewRemovesAShownOffering(t *testing.T) {
	e := newEnv(t)
	member := astral.GenerateIdentity()
	sink := newSink()
	st, _ := e.discoverSwarm(astral.GenerateIdentity(), true, sink, []*astral.Identity{member}, "player")
	r := st.Contributions()[0]
	p, q := astral.GenerateIdentity(), astral.GenerateIdentity()

	r.Update(remoteView(q, "player", true), false)
	r.Update(remoteView(p, "player", true), true)
	expectEvent(t, sink, "update")
	r.Initial(nil)
	expectEvent(t, sink, "boundary")

	r.Update(remoteView(p, "player", true), false)
	if ev := expectEvent(t, sink, "removed"); len(ev.r.Offerings) != 1 || !ev.r.Offerings[0].ProviderID.IsEqual(p) {
		t.Fatalf("removed %v; want p", ev.r.Offerings)
	}
	r.Lost()
	expectNoEvent(t, sink)
}

// In a discovery of every service a member reports any name; its incomplete
// names reach services.incomplete, and a member lost before reporting leaves
// an incomplete outcome with no name.
func TestAllServicesMemberOutcome(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(*Contribution)
		want []string
	}{
		{"reported", func(r *Contribution) { r.Initial([]string{"radio"}) }, []string{"radio"}},
		{"lost", func(r *Contribution) { r.Lost() }, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			sink := newSink()
			st := e.c.Discover(Request{Caller: astral.GenerateIdentity(), All: true, Members: []*astral.Identity{astral.GenerateIdentity()}})
			go st.Run(sink)
			r := st.Contributions()[0]
			r.Update(remoteView(astral.GenerateIdentity(), "anything", true), true)
			expectEvent(t, sink, "update")
			tc.end(r)
			ev := <-sink.events
			if ev.kind == "removed" { // why: a lost member's shown offerings go first
				ev = expectEvent(t, sink, "boundary")
			}
			if ev.kind != "boundary" {
				t.Fatalf("got %s; want the boundary", ev.kind)
			}
			if ev.inc == nil || len(ev.inc.Services) != len(tc.want) {
				t.Fatalf("incomplete %+v; want %v", ev.inc, tc.want)
			}
			for i, s := range tc.want {
				if string(ev.inc.Services[i]) != s {
					t.Fatalf("incomplete %v; want %v", ev.inc.Services, tc.want)
				}
			}
		})
	}
}

// A tagged follow forgets a withdrawn offering, so a list change re-sends
// nothing for it.
func TestTaggedFollowForgetsAWithdrawnOffering(t *testing.T) {
	e := newEnv(t)
	b, tr := e.register("player")
	sink := newSink()
	st := e.c.Discover(Request{Caller: astral.GenerateIdentity(), Names: []string{"player"}, Follow: true, Tagged: true})
	go st.Run(sink)
	answer(t, b, expectAsk(t, tr), true)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")
	if err := b.Change(&services.Change{All: true}); err != nil {
		t.Fatal(err)
	}
	answer(t, b, expectAsk(t, tr), false)
	if ev := expectEvent(t, sink, "update"); bool(ev.u.Available) {
		t.Fatal("want the withdrawal")
	}
	e.c.SetPrivate(&services.PrivateList{Entries: []*services.PrivateEntry{{Service: "player"}}})
	expectNoEvent(t, sink)
}
