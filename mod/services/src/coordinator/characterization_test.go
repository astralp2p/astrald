package coordinator

import (
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// Characterization tests: each pins today's behaviour of a path the readability
// refactor reshapes.

func expectIncomplete(t *testing.T, s *fakeSink, name string) {
	t.Helper()
	b := expectEvent(t, s, "boundary")
	if b.inc == nil || len(b.inc.Services) != 1 || string(b.inc.Services[0]) != name {
		t.Fatalf("boundary %+v; want incomplete %s", b.inc, name)
	}
}

// An initial evaluation waiting behind an outstanding ask leaves its slot
// without failing anything when its stream closes; the evaluation still
// waiting beside it is asked next.
func TestWaitingEvaluationLeavesWithItsStream(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	first, _ := e.discover(caller, true, newSink(), "player")
	defer first.Close()
	old := expectAsk(t, tr)

	leavingSink := newSink()
	leaving, leavingDone := e.discover(caller, false, leavingSink, "player")
	stayingSink := newSink()
	staying, stayingDone := e.discover(caller, false, stayingSink, "player")
	defer staying.Close()
	expectNoAsk(t, tr)
	e.invariants()

	leaving.Close()
	expectDone(t, leavingDone)
	expectNoEvent(t, leavingSink)
	e.invariants()

	answer(t, src, old, true)
	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, stayingSink, "update")
	if b := expectEvent(t, stayingSink, "boundary"); b.inc != nil {
		t.Fatalf("incomplete %+v; want complete", b.inc)
	}
	expectDone(t, stayingDone)
	expectNoAsk(t, tr)
	e.invariants()
}

// An initial evaluation waiting behind an outstanding ask fails when the
// budget expires, and the late answer asks nothing for it.
func TestWaitingEvaluationFailsAtTheBudget(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	first, _ := e.discover(caller, true, newSink(), "player")
	defer first.Close()
	old := expectAsk(t, tr)

	sink := newSink()
	_, done := e.discover(caller, false, sink, "player")
	e.clock.Advance(budget)
	expectIncomplete(t, sink, "player")
	expectDone(t, done)
	e.invariants()

	answer(t, src, old, true)
	expectNoAsk(t, tr)
	e.invariants()
}

// Closing a binding fails both the evaluation in its outstanding ask and the
// evaluation waiting in the slot behind it.
func TestBindingClosesWithEvaluationsWaiting(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	followSink := newSink()
	follow, _ := e.discover(caller, true, followSink, "player")
	defer follow.Close()
	expectAsk(t, tr)

	sink := newSink()
	_, done := e.discover(caller, false, sink, "player")
	src.Close()
	expectIncomplete(t, followSink, "player")
	expectIncomplete(t, sink, "player")
	expectDone(t, done)
	select {
	case <-tr.closed:
	case <-time.After(waitFor):
		t.Fatal("the transport was not closed")
	}
	e.invariants()
}

// With a budget longer than the ask timeout, the timeout fails the initial
// evaluation the ask carried, and the late answer is ignored.
func TestRequestTimeoutFailsTheEvaluationsItCarried(t *testing.T) {
	clock := &fakeClock{}
	e := &env{t: t, clock: clock, c: New(Config{InitialBudget: 2 * timeout, RequestTimeout: timeout, Clock: clock})}
	src, tr := e.register("player")
	sink := newSink()
	st, _ := e.discover(astral.GenerateIdentity(), true, sink, "player")
	defer st.Close()

	a := expectAsk(t, tr)
	e.clock.Advance(timeout)
	expectIncomplete(t, sink, "player")
	answer(t, src, a, true)
	expectNoEvent(t, sink)
	expectNoAsk(t, tr)
	e.invariants()
}

// A timeout for an ask that was already answered changes nothing.
func TestStaleRequestTimeoutIsIgnored(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	sink := newSink()
	st, _ := e.discover(astral.GenerateIdentity(), true, sink, "player")
	defer st.Close()

	a := expectAsk(t, tr)
	e.c.mu.Lock()
	req := src.inflight[a.RequestID]
	e.c.mu.Unlock()
	answer(t, src, a, true)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")

	src.requestExpired(req)
	expectNoEvent(t, sink)
	expectNoAsk(t, tr)
	e.invariants()
}

// A negative view of an offering never shown replaces its queued, unsent
// available view and leaves the queue; the boundary no longer waits for it.
func TestNegativeViewReplacesAnUnsentView(t *testing.T) {
	e := newEnv(t)
	first, firstTr := e.register("player")
	second, secondTr := e.register("player")
	caller := astral.GenerateIdentity()
	sink := newSink()
	sink.gate = make(chan struct{})
	st, _ := e.discover(caller, true, sink, "player")
	defer st.Close()
	defer close(sink.gate)

	answer(t, first, expectAsk(t, firstTr), true)
	if u := expectEvent(t, sink, "update").u; !u.ProviderID.IsEqual(first.Provider()) {
		t.Fatalf("got %v; want the first provider", u.ProviderID)
	}
	answer(t, second, expectAsk(t, secondTr), true)
	if err := second.Change(&services.Change{Callers: []*astral.Identity{caller}}); err != nil {
		t.Fatal(err)
	}
	answer(t, second, expectAsk(t, secondTr), false)
	e.invariants()

	sink.gate <- struct{}{}
	if b := expectEvent(t, sink, "boundary"); b.inc != nil {
		t.Fatalf("incomplete %+v; want complete", b.inc)
	}
	sink.gate <- struct{}{}
	expectNoEvent(t, sink)
}

// failingTransport refuses every ask.
type failingTransport struct {
	once   sync.Once
	closed chan struct{}
}

func (t *failingTransport) Send(*services.Ask) error { return errors.New("send failed") }
func (t *failingTransport) Close()                   { t.once.Do(func() { close(t.closed) }) }

// A failed Transport.Send closes the binding: its initial work fails, the
// transport closes, and its names are released.
func TestTransportSendFailureClosesTheBinding(t *testing.T) {
	e := newEnv(t)
	provider := astral.GenerateIdentity()
	tr := &failingTransport{closed: make(chan struct{})}
	if _, err := e.c.Register(provider, []string{"player"}, tr); err != nil {
		t.Fatal(err)
	}

	sink := newSink()
	_, done := e.discover(astral.GenerateIdentity(), false, sink, "player")
	expectIncomplete(t, sink, "player")
	expectDone(t, done)
	select {
	case <-tr.closed:
	case <-time.After(waitFor):
		t.Fatal("the transport was not closed")
	}
	if _, err := e.c.Register(provider, []string{"player"}, newTransport()); err != nil {
		t.Fatalf("the failed binding still owns its names: %v", err)
	}
	e.invariants()
}

var errWrite = errors.New("write failed")

// failingSink fails every write.
type failingSink struct{}

func (failingSink) Update(*services.Update) error       { return errWrite }
func (failingSink) Removed(*services.Removed) error     { return errWrite }
func (failingSink) Boundary(*services.Incomplete) error { return errWrite }

// A failed write closes the stream, Run returns the error, and the closed
// follow leaves no demand on the provider.
func TestSinkWriteFailureClosesTheStream(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player")
	caller := astral.GenerateIdentity()
	st := e.c.Discover(caller, []string{"player"}, true, nil)
	done := make(chan error, 1)
	go func() { done <- st.Run(failingSink{}) }()

	answer(t, src, expectAsk(t, tr), true)
	select {
	case err := <-done:
		if !errors.Is(err, errWrite) {
			t.Fatalf("Run returned %v; want the write error", err)
		}
	case <-time.After(waitFor):
		t.Fatal("Run did not return")
	}
	select {
	case <-st.Done():
	default:
		t.Fatal("the stream did not close")
	}
	if err := src.Change(&services.Change{Callers: []*astral.Identity{caller}}); err != nil {
		t.Fatal(err)
	}
	expectNoAsk(t, tr)
	e.invariants()
}

// Removals pending together are written as one object, sorted by provider.
func TestRemovalsAreWrittenSorted(t *testing.T) {
	e := newEnv(t)
	a, aTr := e.register("player")
	b, bTr := e.register("player")
	sink := newSink()
	sink.gate = make(chan struct{})
	st, _ := e.discover(astral.GenerateIdentity(), true, sink, "player")
	defer st.Close()
	defer close(sink.gate)

	answer(t, a, expectAsk(t, aTr), true)
	expectEvent(t, sink, "update")
	answer(t, b, expectAsk(t, bTr), true)
	sink.gate <- struct{}{}
	expectEvent(t, sink, "update")
	sink.gate <- struct{}{}
	expectEvent(t, sink, "boundary")

	a.Close()
	b.Close()
	sink.gate <- struct{}{}
	r := expectEvent(t, sink, "removed").r
	want := []string{a.Provider().String(), b.Provider().String()}
	sort.Strings(want)
	if len(r.Offerings) != 2 || r.Offerings[0].ProviderID.String() != want[0] || r.Offerings[1].ProviderID.String() != want[1] {
		t.Fatalf("removed %+v; want both providers in sorted order", r.Offerings)
	}
}

// A member removal with a nil key or no provider is skipped, and a removal on
// a closed stream does nothing.
func TestSwarmRemovalWithNilKeyOrOnAClosedStream(t *testing.T) {
	e := newEnv(t)
	sink := newSink()
	st, _ := e.discoverSwarm(astral.GenerateIdentity(), true, sink, []*astral.Identity{astral.GenerateIdentity()}, "player")
	r := st.Contributions()[0]
	p := astral.GenerateIdentity()
	r.Update(remoteView(p, "player", true))
	r.Initial(nil)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")

	r.Remove([]*services.OfferingKey{nil, {Name: "player"}})
	expectNoEvent(t, sink)
	e.invariants()

	st.Close()
	r.Remove([]*services.OfferingKey{{ProviderID: p, Name: "player"}})
	expectNoEvent(t, sink)
	e.invariants()
}

// A closing binding retracts only the services the stream requested.
func TestClosedBindingRetractsOnlyRequestedServices(t *testing.T) {
	e := newEnv(t)
	src, tr := e.register("player", "wallet")
	sink := newSink()
	st, _ := e.discover(astral.GenerateIdentity(), true, sink, "player")
	defer st.Close()

	answer(t, src, expectAsk(t, tr), true)
	expectEvent(t, sink, "update")
	expectEvent(t, sink, "boundary")

	src.Close()
	r := expectEvent(t, sink, "removed").r
	if len(r.Offerings) != 1 || r.Offerings[0].Name != "player" {
		t.Fatalf("removed %+v; want player only", r.Offerings)
	}
	expectNoEvent(t, sink)
	e.invariants()
}
