package coordinator

import (
	"testing"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// 4.3: the budget ends the initial attempt as incomplete; the follow stays open
// and a late answer to the still-active ask is delivered.
func TestBudgetEndsInitialAttempt(t *testing.T) {
	e := newEnv(t)
	quietSrc, quietTr := e.register("player")
	fast, fastTr := e.register("player")
	caller := astral.GenerateIdentity()
	sink := newSink()
	st, _ := e.discover(caller, true, sink, "player")
	defer st.Close()

	late := expectAsk(t, quietTr)
	answer(t, fast, expectAsk(t, fastTr), true)
	expectEvent(t, sink, "update")
	expectNoEvent(t, sink)

	e.clock.Advance(budget)
	b := expectEvent(t, sink, "boundary")
	if b.inc == nil || len(b.inc.Services) != 1 || b.inc.Services[0] != "player" {
		t.Fatalf("boundary %+v; want incomplete player", b.inc)
	}
	e.invariants()

	answer(t, quietSrc, late, true)
	expectEvent(t, sink, "update")
}

// 4.3: a request timeout retires the ask, fails its initial work, ignores the
// late answer, and frees the slot.
func TestRequestTimeout(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	sink := newSink()
	st, _ := e.discover(caller, true, sink, "player")
	defer st.Close()

	a := expectAsk(t, tr)
	e.clock.Advance(timeout)
	if b := expectEvent(t, sink, "boundary"); b.inc == nil {
		t.Fatal("timed-out attempt reported complete")
	}
	answer(t, src, a, true)
	expectNoEvent(t, sink)

	if err := src.Change(&services.Change{Callers: []*astral.Identity{caller}}); err != nil {
		t.Fatal(err)
	}
	b := expectAsk(t, tr)
	if b.RequestID == a.RequestID {
		t.Fatal("request ID reused on one binding")
	}
	e.invariants()
}

// Budget expiry, request timeout and source close all reach the same
// obligation; it ends once and the boundary is sent once.
func TestFailurePathsEndAnObligationOnce(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	sink := newSink()
	st, _ := e.discover(astral.GenerateIdentity(), true, sink, "player")
	defer st.Close()

	expectAsk(t, tr)
	e.clock.Advance(budget)
	e.clock.Advance(timeout)
	src.Close()

	b := expectEvent(t, sink, "boundary")
	if b.inc == nil {
		t.Fatal("want incomplete")
	}
	expectNoEvent(t, sink)
	e.invariants()
}

// 4.4: closing a binding removes only offerings the stream saw.
func TestBindingCloses(t *testing.T) {
	e := newEnv(t)
	lost, lostTr := e.register("player", "bitcoin-wallet")
	kept, keptTr := e.register("player")
	caller := astral.GenerateIdentity()
	sink := newSink()
	st, _ := e.discover(caller, true, sink, "player", "bitcoin-wallet")
	defer st.Close()

	// lost asks bitcoin-wallet first (name order), then player.
	wallet := expectAsk(t, lostTr)
	answer(t, lost, wallet, false)
	answer(t, lost, expectAsk(t, lostTr), true)
	answer(t, kept, expectAsk(t, keptTr), true)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")

	lost.Close()
	r := expectEvent(t, sink, "removed").r
	if len(r.Offerings) != 1 || r.Offerings[0].Name != "player" || !r.Offerings[0].ProviderID.IsEqual(lost.Provider()) {
		t.Fatalf("removed %+v; want only the shown player offering", r.Offerings)
	}
	e.invariants()
}

// 4.4 + S8: a view in flight when its source closes is followed by a removal,
// and the stream does not keep it as shown.
func TestCloseWhileViewInFlight(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	sink := newSink()
	sink.gate = make(chan struct{})
	st, _ := e.discover(astral.GenerateIdentity(), true, sink, "player")
	defer st.Close()

	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sink, "update") // writer now blocked inside the send
	src.Close()
	sink.gate <- struct{}{}

	expectEvent(t, sink, "removed")
	sink.gate <- struct{}{}
	expectEvent(t, sink, "boundary")
	sink.gate <- struct{}{}

	e.c.mu.Lock()
	n := len(st.visible)
	e.c.mu.Unlock()
	if n != 0 {
		t.Fatalf("stream still shows %d offerings after the removal", n)
	}
}

// 4.5: re-registration after loss produces a fresh view after the removal.
func TestReRegistration(t *testing.T) {
	e := newEnv(t)
	provider := astral.GenerateIdentity()
	tr := newTransport()
	src, err := e.c.Register(provider, []string{"player"}, tr)
	if err != nil {
		t.Fatal(err)
	}
	sink := newSink()
	st, _ := e.discover(astral.GenerateIdentity(), true, sink, "player")
	defer st.Close()
	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")

	if _, err := e.c.Register(provider, []string{"player"}, newTransport()); err != ErrOwned {
		t.Fatalf("overlapping registration: %v; want ErrOwned", err)
	}
	src.Close()
	expectEvent(t, sink, "removed")

	tr2 := newTransport()
	src2, err := e.c.Register(provider, []string{"player"}, tr2)
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	answer(t, src2, expectAsk(t, tr2), true)
	expectEvent(t, sink, "update")
	e.invariants()
}

// T4: admission is all-or-nothing.
func TestRegistrationIsAtomic(t *testing.T) {
	e := newEnv(t)
	provider := astral.GenerateIdentity()
	if _, err := e.c.Register(provider, []string{"player"}, newTransport()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Register(provider, []string{"bitcoin-wallet", "player"}, newTransport()); err != ErrOwned {
		t.Fatalf("got %v; want ErrOwned", err)
	}
	if _, err := e.c.Register(provider, []string{"bitcoin-wallet"}, newTransport()); err != nil {
		t.Fatalf("the failed attempt kept bitcoin-wallet: %v", err)
	}
}

// Malformed provider messages fail the binding.
func TestMalformedMessagesFailTheSource(t *testing.T) {
	for name, send := range map[string]func(*Source, *services.Ask) error{
		"wrong service": func(src *Source, a *services.Ask) error {
			return src.Answer(&services.Answer{RequestID: a.RequestID, Update: &services.Update{Name: "other"}})
		},
		"foreign provider": func(src *Source, a *services.Ask) error {
			return src.Answer(&services.Answer{RequestID: a.RequestID, Update: &services.Update{Name: a.Service, ProviderID: astral.GenerateIdentity()}})
		},
		"missing update": func(src *Source, a *services.Ask) error {
			return src.Answer(&services.Answer{RequestID: a.RequestID})
		},
		"empty change": func(src *Source, _ *services.Ask) error {
			return src.Change(&services.Change{})
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			src, tr := e.register("player")
			sink := newSink()
			st, _ := e.discover(astral.GenerateIdentity(), true, sink, "player")
			defer st.Close()
			if err := send(src, expectAsk(t, tr)); err != ErrMalformed {
				t.Fatalf("got %v; want ErrMalformed", err)
			}
			select {
			case <-tr.closed:
			default:
				t.Fatal("transport left open")
			}
			if b := expectEvent(t, sink, "boundary"); b.inc == nil {
				t.Fatal("want incomplete after the source failed")
			}
			e.invariants()
		})
	}
}
