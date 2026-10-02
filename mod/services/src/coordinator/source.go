package coordinator

import (
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// Source is one provider lifetime: an app binding or a native registration.
// Its names are fixed for the lifetime.
type Source struct {
	c        *Coordinator
	provider *astral.Identity
	names    []string // sorted
	t        Transport

	asks    []*services.Ask
	askWake chan struct{}
	issued  map[astral.Nonce]struct{} // every RequestID ever issued; never shrinks
	pending map[astral.Nonce]*request
	slots   map[string]*slot // by caller
	closed  bool
	done    chan struct{}
}

// slot is the T2 gate: one outstanding ask per source and caller.
type slot struct {
	src     *Source
	caller  *astral.Identity
	active  *request
	initial map[string][]*obligation // service → obligations waiting for a fresh ask
	dirty   map[string]struct{}      // service → retained refresh work
}

type request struct {
	id      astral.Nonce
	slot    *slot
	service string
	obls    []*obligation
	timer   Timer
}

func newSource(c *Coordinator, provider *astral.Identity, names []string, t Transport) *Source {
	return &Source{
		c:        c,
		provider: provider,
		names:    names,
		t:        t,
		askWake:  make(chan struct{}, 1),
		issued:   map[astral.Nonce]struct{}{},
		pending:  map[astral.Nonce]*request{},
		slots:    map[string]*slot{},
		done:     make(chan struct{}),
	}
}

// Provider is the identity the source answers as.
func (src *Source) Provider() *astral.Identity { return src.provider }

// Names are the services of the source, sorted.
func (src *Source) Names() []string { return append([]string(nil), src.names...) }

// Done is closed when the source closes.
func (src *Source) Done() <-chan struct{} { return src.done }

func (src *Source) slot(caller *astral.Identity) *slot {
	ck := caller.String()
	sl := src.slots[ck]
	if sl == nil {
		sl = &slot{src: src, caller: caller, initial: map[string][]*obligation{}, dirty: map[string]struct{}{}}
		src.slots[ck] = sl
	}
	return sl
}

// schedule issues the next ask of a free slot: initial work before refresh
// work, services in name order.
func (src *Source) schedule(sl *slot) {
	if src.closed || sl.active != nil {
		return
	}
	service, ok := sl.next()
	if !ok {
		src.collect(sl)
		return
	}

	req := &request{id: src.nextID(), slot: sl, service: service}
	for _, o := range sl.initial[service] {
		o.req = req
		req.obls = append(req.obls, o)
	}
	delete(sl.initial, service)
	delete(sl.dirty, service)

	sl.active = req
	src.pending[req.id] = req
	req.timer = src.c.cfg.Clock.AfterFunc(src.c.cfg.RequestTimeout, func() { src.requestExpired(req) })

	src.asks = append(src.asks, &services.Ask{RequestID: req.id, CallerID: sl.caller, Service: astral.String8(service)})
	wake(src.askWake)
}

func (sl *slot) next() (string, bool) {
	for _, name := range sl.src.names {
		if len(sl.initial[name]) > 0 {
			return name, true
		}
	}
	for _, name := range sl.src.names {
		if _, ok := sl.dirty[name]; ok {
			return name, true
		}
	}
	return "", false
}

// collect deletes an idle slot. A slot holds nothing beyond its work.
func (src *Source) collect(sl *slot) {
	if sl.active == nil && len(sl.initial) == 0 && len(sl.dirty) == 0 {
		delete(src.slots, sl.caller.String())
	}
}

// nextID draws a random request ID that is not zero and was never issued on
// this source.
func (src *Source) nextID() astral.Nonce {
	for {
		id := astral.NewNonce()
		if _, used := src.issued[id]; id != 0 && !used {
			src.issued[id] = struct{}{}
			return id
		}
	}
}

func (src *Source) retire(req *request) {
	delete(src.pending, req.id)
	req.timer.Stop()
	req.slot.active = nil
}

func (src *Source) requestExpired(req *request) {
	c := src.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if src.pending[req.id] != req {
		return
	}
	src.retire(req)
	for _, o := range append([]*obligation(nil), req.obls...) {
		o.fail()
	}
	src.schedule(req.slot)
}

// sendLoop drains asks to the transport until the source closes.
func (src *Source) sendLoop() {
	c := src.c
	for {
		c.mu.Lock()
		if src.closed {
			c.mu.Unlock()
			return
		}
		if len(src.asks) == 0 {
			c.mu.Unlock()
			select {
			case <-src.askWake:
			case <-src.done:
			}
			continue
		}
		ask := src.asks[0]
		src.asks = src.asks[1:]
		c.mu.Unlock()

		if err := src.t.Send(ask); err != nil {
			src.Close()
			return
		}
	}
}

func wake(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
