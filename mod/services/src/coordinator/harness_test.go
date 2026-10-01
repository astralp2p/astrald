package coordinator

import (
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

const (
	budget  = 5 * time.Second
	timeout = 10 * time.Second
	waitFor = 2 * time.Second
	quiet   = 50 * time.Millisecond
)

// fakeClock fires timers only when a test advances it.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []*fakeTimer
}

type fakeTimer struct {
	c       *fakeClock
	at      time.Duration
	f       func()
	stopped bool
	fired   bool
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, at: c.now + d, f: f}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := !t.stopped && !t.fired
	t.stopped = true
	return was
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now += d
	var due []*fakeTimer
	for _, t := range c.timers {
		if !t.stopped && !t.fired && t.at <= c.now {
			t.fired = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		t.f()
	}
}

// fakeTransport hands asks to the test.
type fakeTransport struct {
	asks   chan *services.Ask
	once   sync.Once
	closed chan struct{}
}

func newTransport() *fakeTransport {
	return &fakeTransport{asks: make(chan *services.Ask, 64), closed: make(chan struct{})}
}

func (t *fakeTransport) Send(a *services.Ask) error { t.asks <- a; return nil }
func (t *fakeTransport) Close()                     { t.once.Do(func() { close(t.closed) }) }

type event struct {
	kind string // "update", "removed", "boundary"
	u    *services.Update
	r    *services.Removed
	inc  *services.Incomplete
}

// fakeSink records writes; with a gate, each write waits for one release.
type fakeSink struct {
	events chan event
	gate   chan struct{}
}

func newSink() *fakeSink { return &fakeSink{events: make(chan event, 64)} }

func (s *fakeSink) put(e event) error {
	s.events <- e
	if s.gate != nil {
		<-s.gate
	}
	return nil
}

func (s *fakeSink) Update(u *services.Update) error   { return s.put(event{kind: "update", u: u}) }
func (s *fakeSink) Removed(r *services.Removed) error { return s.put(event{kind: "removed", r: r}) }
func (s *fakeSink) Boundary(inc *services.Incomplete) error {
	return s.put(event{kind: "boundary", inc: inc})
}

type env struct {
	t     *testing.T
	c     *Coordinator
	clock *fakeClock
}

func newEnv(t *testing.T) *env {
	clock := &fakeClock{}
	return &env{t: t, clock: clock, c: New(Config{InitialBudget: budget, RequestTimeout: timeout, Clock: clock})}
}

func (e *env) register(names ...string) (*Source, *fakeTransport) {
	e.t.Helper()
	tr := newTransport()
	src, err := e.c.Register(astral.GenerateIdentity(), names, tr)
	if err != nil {
		e.t.Fatalf("register %v: %v", names, err)
	}
	return src, tr
}

// discover starts a stream and its writer; the returned channel yields Run's
// result.
func (e *env) discover(caller *astral.Identity, follow bool, sink *fakeSink, names ...string) (*Stream, chan error) {
	st := e.c.Discover(caller, names, follow)
	done := make(chan error, 1)
	go func() { done <- st.Run(sink) }()
	return st, done
}

func expectAsk(t *testing.T, tr *fakeTransport) *services.Ask {
	t.Helper()
	select {
	case a := <-tr.asks:
		return a
	case <-time.After(waitFor):
		t.Fatal("no ask arrived")
		return nil
	}
}

func expectNoAsk(t *testing.T, tr *fakeTransport) {
	t.Helper()
	select {
	case a := <-tr.asks:
		t.Fatalf("unexpected ask for %v / %v", a.Service, a.CallerID)
	case <-time.After(quiet):
	}
}

func expectEvent(t *testing.T, s *fakeSink, kind string) event {
	t.Helper()
	select {
	case e := <-s.events:
		if e.kind != kind {
			t.Fatalf("got %s event, want %s (%+v)", e.kind, kind, e)
		}
		return e
	case <-time.After(waitFor):
		t.Fatalf("no %s event arrived", kind)
		return event{}
	}
}

func expectNoEvent(t *testing.T, s *fakeSink) {
	t.Helper()
	select {
	case e := <-s.events:
		t.Fatalf("unexpected %s event: %+v", e.kind, e)
	case <-time.After(quiet):
	}
}

func expectDone(t *testing.T, done chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stream ended with %v", err)
		}
	case <-time.After(waitFor):
		t.Fatal("stream did not end")
	}
}

func answer(t *testing.T, src *Source, a *services.Ask, available bool) {
	t.Helper()
	err := src.Answer(&services.Answer{RequestID: a.RequestID, Update: &services.Update{Available: astral.Bool(available), Name: a.Service}})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
}

// invariants checks plan §6.3 under the coordinator lock.
func (e *env) invariants() {
	e.t.Helper()
	c := e.c
	c.mu.Lock()
	defer c.mu.Unlock()

	for k, src := range c.owners {
		if src.closed {
			e.t.Fatalf("closed source still owns %v", k)
		}
		for _, sl := range src.slots {
			if sl.active != nil && src.pending[sl.active.id] != sl.active {
				e.t.Fatalf("active request %v not pending", sl.active.id)
			}
		}
		for id := range src.pending {
			if _, ok := src.issued[id]; !ok || id == 0 {
				e.t.Fatalf("pending request %v was not issued", id)
			}
		}
	}
	for _, streams := range c.byCaller {
		for st := range streams {
			e.streamInvariants(st)
		}
	}
}

func (e *env) streamInvariants(st *Stream) {
	e.t.Helper()
	if at := st.attempt; at != nil {
		for o := range at.open {
			in := 0
			if o.req != nil && contains(o.req.obls, o) {
				in++
			}
			if contains(o.slot.initial[o.service], o) {
				in++
			}
			if in != 1 {
				e.t.Fatalf("obligation for %s held in %d places", o.service, in)
			}
		}
		if at.resolved && len(at.open) != 0 {
			e.t.Fatal("finished attempt still has open obligations")
		}
		for k := range at.owed {
			_, queued := st.pending[k]
			if !queued && (st.inflight == nil || *st.inflight != k) {
				e.t.Fatalf("owed view %v neither queued nor in flight", k)
			}
		}
	}
}

func contains(list []*obligation, o *obligation) bool {
	for _, x := range list {
		if x == o {
			return true
		}
	}
	return false
}
