package coordinator

import (
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// Source is one provider lifetime: an app binding or a native registration.
// Its names are fixed for the lifetime.
type Binding struct {
	c        *Coordinator
	provider *astral.Identity
	names    []string // sorted
	t        Transport

	sendQueue []*services.Ask
	sendWake  chan struct{}
	usedIDs   map[astral.Nonce]struct{} // every RequestID ever issued; never shrinks
	inflight  map[astral.Nonce]*request
	slots     map[string]*askSlot // by caller
	closed    bool
	done      chan struct{}
}

// slot is the T2 gate: one outstanding ask per source and caller.
type askSlot struct {
	binding    *Binding
	caller     *astral.Identity
	active     *request
	waiting    map[string][]*evaluation // service → obligations waiting for a fresh ask
	reevaluate map[string]struct{}      // service → retained refresh work
}

type request struct {
	id          astral.Nonce
	slot        *askSlot
	service     string
	evaluations []*evaluation
	timer       Timer
}

func newBinding(c *Coordinator, provider *astral.Identity, names []string, t Transport) *Binding {
	return &Binding{
		c:        c,
		provider: provider,
		names:    names,
		t:        t,
		sendWake: make(chan struct{}, 1),
		usedIDs:  map[astral.Nonce]struct{}{},
		inflight: map[astral.Nonce]*request{},
		slots:    map[string]*askSlot{},
		done:     make(chan struct{}),
	}
}

// Provider is the identity the source answers as.
func (b *Binding) Provider() *astral.Identity { return b.provider }

func (b *Binding) slot(caller *astral.Identity) *askSlot {
	ck := callerKey(caller)
	sl := b.slots[ck]
	if sl == nil {
		sl = &askSlot{binding: b, caller: caller, waiting: map[string][]*evaluation{}, reevaluate: map[string]struct{}{}}
		b.slots[ck] = sl
	}
	return sl
}

// schedule issues the next ask of a free slot: initial work before refresh
// work, services in name order.
func (b *Binding) schedule(sl *askSlot) {
	if b.closed || sl.active != nil {
		return
	}
	service, ok := sl.nextName()
	if !ok {
		b.removeIfIdle(sl)
		return
	}

	req := &request{id: b.newRequestID(), slot: sl, service: service}
	for _, o := range sl.waiting[service] {
		o.req = req
		req.evaluations = append(req.evaluations, o)
	}
	delete(sl.waiting, service)
	delete(sl.reevaluate, service)

	sl.active = req
	b.inflight[req.id] = req
	req.timer = b.c.cfg.Clock.AfterFunc(b.c.cfg.RequestTimeout, func() { b.requestExpired(req) })

	b.sendQueue = append(b.sendQueue, &services.Ask{RequestID: req.id, CallerID: sl.caller, Service: astral.String8(service)})
	wake(b.sendWake)
}

func (sl *askSlot) nextName() (string, bool) {
	for _, name := range sl.binding.names {
		if len(sl.waiting[name]) > 0 {
			return name, true
		}
	}
	for _, name := range sl.binding.names {
		if _, ok := sl.reevaluate[name]; ok {
			return name, true
		}
	}
	return "", false
}

// collect deletes an idle slot. A slot holds nothing beyond its work.
func (b *Binding) removeIfIdle(sl *askSlot) {
	if sl.active == nil && len(sl.waiting) == 0 && len(sl.reevaluate) == 0 {
		delete(b.slots, callerKey(sl.caller))
	}
}

// nextID draws a random request ID that is not zero and was never issued on
// this source.
func (b *Binding) newRequestID() astral.Nonce {
	for {
		id := astral.NewNonce()
		if _, used := b.usedIDs[id]; id != 0 && !used {
			b.usedIDs[id] = struct{}{}
			return id
		}
	}
}

func (b *Binding) removeRequest(req *request) {
	delete(b.inflight, req.id)
	req.timer.Stop()
	req.slot.active = nil
}

func (b *Binding) requestExpired(req *request) {
	c := b.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if b.inflight[req.id] != req {
		return
	}
	b.removeRequest(req)
	failAll(req.evaluations)
	b.schedule(req.slot)
}

// sendLoop drains asks to the transport until the source closes.
func (b *Binding) sendLoop() {
	c := b.c
	for {
		c.mu.Lock()
		if b.closed {
			c.mu.Unlock()
			return
		}
		if len(b.sendQueue) == 0 {
			c.mu.Unlock()
			select {
			case <-b.sendWake:
			case <-b.done:
			}
			continue
		}
		ask := b.sendQueue[0]
		b.sendQueue = b.sendQueue[1:]
		c.mu.Unlock()

		if err := b.t.Send(ask); err != nil {
			b.Close()
			return
		}
	}
}

func failAll(list []*evaluation) {
	for _, o := range append([]*evaluation(nil), list...) {
		o.fail()
	}
}

func endAll(list []*evaluation) {
	for _, o := range append([]*evaluation(nil), list...) {
		o.end()
	}
}

func wake(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
