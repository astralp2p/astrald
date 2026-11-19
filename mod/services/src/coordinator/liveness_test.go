package coordinator

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// Liveness: probes of followed bindings and retirement after unanswered asks.
// Spec: .ai/system/protocols/services/ops/services.advertise.md#liveness

const (
	probe       = 60 * time.Second
	maxTimeouts = 2
)

func newLiveEnv(t *testing.T) *env {
	clock := &fakeClock{}
	return &env{t: t, clock: clock, c: New(Config{
		InitialBudget:  budget,
		RequestTimeout: timeout,
		ProbeInterval:  probe,
		MaxAskTimeouts: maxTimeouts,
		Clock:          clock,
	})}
}

// followShown registers a provider of player and a follower that has been
// shown its available offering, at time 0.
func (e *env) followShown() (*Binding, *fakeTransport, *fakeSink, *astral.Identity) {
	e.t.Helper()
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	sink := newSink()
	st, _ := e.discover(caller, true, sink, "player")
	e.t.Cleanup(st.Close)
	answer(e.t, src, expectAsk(e.t, tr), true)
	expectEvent(e.t, sink, "update")
	expectEvent(e.t, sink, "boundary")
	return src, tr, sink, caller
}

func answerInfo(t *testing.T, src *Binding, a *services.Ask, info string) {
	t.Helper()
	err := src.Answer(&services.Answer{RequestID: a.RequestID, Update: &services.Update{Available: true, Name: a.Service, Info: bundleOf(t, info)}})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
}

func bundleOf(t *testing.T, s string) *astral.Bundle {
	t.Helper()
	b := astral.NewBundle()
	if err := b.Append(astral.NewString8(s)); err != nil {
		t.Fatal(err)
	}
	return b
}

func isClosed(tr *fakeTransport) bool {
	select {
	case <-tr.closed:
		return true
	default:
		return false
	}
}

func (e *env) owns(src *Binding, name string) bool {
	e.c.mu.Lock()
	defer e.c.mu.Unlock()
	return e.c.owners[keyOf(src.Provider(), name)] == src
}

// A follower is shown a provider that then stops answering. The first probe
// ask times out and changes nothing; the second ends the binding: the follower
// receives services.removed with binding_closed, the transport closes and the
// names are free again, all within 60 s + 10 s + 60 s + 10 s.
func TestHungProviderIsRetired(t *testing.T) {
	e := newLiveEnv(t)
	src, tr, sink, _ := e.followShown()

	e.clock.Advance(probe) // t=60
	expectAsk(t, tr)
	e.clock.Advance(timeout) // t=70: timeout 1 of 2
	expectNoEvent(t, sink)
	if isClosed(tr) || !e.owns(src, "player") {
		t.Fatal("the binding ended after one unanswered ask")
	}

	e.clock.Advance(probe - timeout) // t=120
	expectAsk(t, tr)
	e.clock.Advance(timeout) // t=130: timeout 2 of 2
	r := expectEvent(t, sink, "removed").r
	if len(r.Offerings) != 1 || !r.Offerings[0].ProviderID.IsEqual(src.Provider()) || services.RemovalCause(r.Cause) != services.CauseBindingClosed {
		t.Fatalf("removed %+v cause %v; want the provider's player with binding_closed", r.Offerings, r.Cause)
	}
	if !isClosed(tr) {
		t.Fatal("the retired binding left its transport open")
	}
	if _, err := e.c.Register(src.Provider(), []string{"player"}, true, newTransport()); err != nil {
		t.Fatalf("register after retirement: %v", err)
	}
	e.clock.Advance(probe) // t=190: a retired binding asks nothing more
	expectNoAsk(t, tr)
	e.invariants()
}

// A provider that answers every probe within 10 s stays registered, and an
// unchanged answer sends its follower nothing.
func TestAnsweringProviderStaysRegistered(t *testing.T) {
	e := newLiveEnv(t)
	src, tr, sink, _ := e.followShown()

	e.clock.Advance(probe) // t=60: the first probe
	for i := 0; i < 10; i++ {
		a := expectAsk(t, tr)
		e.clock.Advance(9 * time.Second)
		answer(t, src, a, true)
		expectNoEvent(t, sink)
		expectNoAsk(t, tr)
		e.clock.Advance(probe - 9*time.Second) // the next probe
	}
	if isClosed(tr) || !e.owns(src, "player") {
		t.Fatal("an answering provider was retired")
	}
	e.invariants()
}

// A follower's re-evaluation answered at 12 s is ignored; the next probe asks
// again, and its answer at 1 s reaches the follower.
func TestLateReevaluationIsAskedAgainByTheNextProbe(t *testing.T) {
	e := newLiveEnv(t)
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	sink := newSink()
	st, _ := e.discover(caller, true, sink, "player")
	defer st.Close()
	answerInfo(t, src, expectAsk(t, tr), "v1")
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")

	if err := src.Change(&services.Change{Callers: []*astral.Identity{caller}}); err != nil {
		t.Fatal(err)
	}
	late := expectAsk(t, tr)
	e.clock.Advance(12 * time.Second)
	answerInfo(t, src, late, "v2")
	expectNoEvent(t, sink)

	e.clock.Advance(probe - 12*time.Second) // t=60
	again := expectAsk(t, tr)
	e.clock.Advance(time.Second)
	answerInfo(t, src, again, "v2")
	u := expectEvent(t, sink, "update").u
	if !sameView(u, &services.Update{Available: true, Name: "player", ProviderID: src.Provider(), Info: bundleOf(t, "v2")}) {
		t.Fatalf("got %+v; want the v2 view", u)
	}
	e.invariants()
}

// A valid answer resets the count: timeout, answer, timeout keeps the binding.
func TestAnswerResetsTheTimeoutCount(t *testing.T) {
	e := newLiveEnv(t)
	src, tr, sink, _ := e.followShown()

	e.clock.Advance(probe) // t=60
	expectAsk(t, tr)
	e.clock.Advance(timeout)         // t=70: timeout 1
	e.clock.Advance(probe - timeout) // t=120
	answer(t, src, expectAsk(t, tr), true)
	e.clock.Advance(probe) // t=180
	expectAsk(t, tr)
	e.clock.Advance(timeout) // t=190: timeout 1 again

	expectNoEvent(t, sink)
	if isClosed(tr) || !e.owns(src, "player") {
		t.Fatal("the binding ended although an answer came between the timeouts")
	}
	e.invariants()
}

// blockingTransport stands in for a native registration whose Evaluator never
// returns: the first Send blocks until release closes.
type blockingTransport struct {
	sent    chan *services.Ask
	release chan struct{}
}

func (t *blockingTransport) Send(a *services.Ask) error {
	t.sent <- a
	<-t.release
	return nil
}
func (t *blockingTransport) Close()     {}
func (t *blockingTransport) InProcess() {}

// A native binding whose evaluation blocks is still registered after 3
// timeouts, and is never probed.
func TestNativeBindingIsNeitherProbedNorRetired(t *testing.T) {
	e := newLiveEnv(t)
	tr := &blockingTransport{sent: make(chan *services.Ask, 8), release: make(chan struct{})}
	defer close(tr.release)
	src, err := e.c.Register(astral.GenerateIdentity(), []string{"nat"}, true, tr)
	if err != nil {
		t.Fatal(err)
	}
	caller := astral.GenerateIdentity()
	sink := newSink()
	st, _ := e.discover(caller, true, sink, "nat")
	defer st.Close()
	<-tr.sent // the sender is now blocked inside the evaluation

	change := &services.Change{Callers: []*astral.Identity{caller}}
	e.clock.Advance(timeout) // timeout 1: the initial ask
	expectEvent(t, sink, "boundary")
	for i := 2; i <= 3; i++ {
		if err := src.Change(change); err != nil {
			t.Fatal(err)
		}
		e.clock.Advance(timeout) // timeout i
	}
	e.clock.Advance(3 * probe)

	if !e.owns(src, "nat") {
		t.Fatal("a native binding was retired")
	}
	e.c.mu.Lock()
	timeouts, armed, queued := src.timeouts, src.probe != nil, len(src.sendQueue)
	e.c.mu.Unlock()
	if timeouts != 3 {
		t.Fatalf("counted %d timeouts; want 3", timeouts)
	}
	if armed || queued != 2 {
		t.Fatalf("probe armed %v, %d asks queued; want no probe and only the 2 asks of the changes", armed, queued)
	}
	expectNoEvent(t, sink)
}

// A probe that fires after the binding closed asks nothing.
func TestProbeAfterCloseDoesNothing(t *testing.T) {
	e := newLiveEnv(t)
	src, tr, sink, _ := e.followShown()

	src.Close()
	expectEvent(t, sink, "removed")
	src.probeTick() // as if the timer had fired just before Close stopped it
	e.clock.Advance(probe)
	expectNoAsk(t, tr)
	expectNoEvent(t, sink)

	e.c.mu.Lock()
	armed := src.probe != nil
	e.c.mu.Unlock()
	if armed {
		t.Fatal("a closed binding re-armed its probe")
	}
	e.invariants()
}

// A binding registered after a follower exists is probed too.
func TestBindingRegisteredUnderAFollowerIsProbed(t *testing.T) {
	e := newLiveEnv(t)
	sink := newSink()
	st, _ := e.discover(astral.GenerateIdentity(), true, sink, "player")
	defer st.Close()
	expectEvent(t, sink, "boundary")

	src, tr := e.register("player")
	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sink, "update")

	e.clock.Advance(probe)
	expectAsk(t, tr)
}
