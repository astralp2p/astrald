package coordinator

import (
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// Binding is one provider lifetime: an app's advertise binding or a native
// registration. Its names are fixed for the lifetime.
type Binding struct {
	c        *Coordinator
	provider *astral.Identity
	names    []string // sorted
	open     bool     // advertised open; the private list can still hide it
	native   bool     // answered in-process; never probed or ended for unanswered asks
	t        Transport

	sendQueue []*services.Ask
	sendWake  chan struct{}
	usedIDs   map[astral.Nonce]struct{} // every RequestID ever issued; never shrinks
	inflight  map[astral.Nonce]*request
	slots     map[string]*askSlot // by caller
	probe     Timer               // the next probe of followed names; nil when none is armed
	timeouts  int                 // consecutive asks retired unanswered
	closed    bool
	done      chan struct{}
}

// askSlot holds one outstanding ask per binding and caller. Work for the caller
// waits here until the active ask is answered or expires.
type askSlot struct {
	binding    *Binding
	caller     *astral.Identity
	active     *request
	waiting    map[string][]*evaluation // service → initial evaluations waiting for a fresh ask
	reevaluate map[string]bool          // service → re-evaluation owed to followers; true when only a probe owes it
}

// request is one ask sent to the provider and not yet answered.
type request struct {
	id          astral.Nonce
	slot        *askSlot
	service     string
	evaluations []*evaluation
	probe       bool // only a probe owes the ask; an unchanged answer reaches no follower
	timer       Timer
}

func newBinding(c *Coordinator, provider *astral.Identity, names []string, t Transport) *Binding {
	_, native := t.(InProcess)
	return &Binding{
		c:        c,
		provider: provider,
		names:    names,
		native:   native,
		t:        t,
		sendWake: make(chan struct{}, 1),
		usedIDs:  map[astral.Nonce]struct{}{},
		inflight: map[astral.Nonce]*request{},
		slots:    map[string]*askSlot{},
		done:     make(chan struct{}),
	}
}

// Provider is the identity the binding answers as.
func (b *Binding) Provider() *astral.Identity { return b.provider }

func (b *Binding) slot(caller *astral.Identity) *askSlot {
	ck := callerKey(caller)
	sl := b.slots[ck]
	if sl == nil {
		sl = &askSlot{binding: b, caller: caller, waiting: map[string][]*evaluation{}, reevaluate: map[string]bool{}}
		b.slots[ck] = sl
	}
	return sl
}

// schedule issues the next ask of a free slot: initial evaluations before
// re-evaluations, services in name order.
func (b *Binding) schedule(sl *askSlot) {
	if b.closed || sl.active != nil {
		return
	}
	service, ok := sl.nextName()
	if !ok {
		b.removeIfIdle(sl)
		return
	}

	req := &request{id: b.newRequestID(), slot: sl, service: service, probe: sl.reevaluate[service]}
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

// owe queues a re-evaluation of service. A re-evaluation that anything other
// than a probe owes stays owed to it when a probe joins.
func (sl *askSlot) owe(service string, probe bool) {
	if owed, ok := sl.reevaluate[service]; ok {
		probe = probe && owed
	}
	sl.reevaluate[service] = probe
}

// nextName picks the service the slot asks about next.
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

// removeIfIdle deletes a slot with no work. A slot holds nothing beyond its
// work.
func (b *Binding) removeIfIdle(sl *askSlot) {
	if sl.active == nil && len(sl.waiting) == 0 && len(sl.reevaluate) == 0 {
		delete(b.slots, callerKey(sl.caller))
	}
}

// newRequestID draws a random request ID that is not zero and was never issued
// on this binding.
func (b *Binding) newRequestID() astral.Nonce {
	for {
		id := astral.NewNonce()
		if _, used := b.usedIDs[id]; id != 0 && !used {
			b.usedIDs[id] = struct{}{}
			return id
		}
	}
}

// removeRequest ends an outstanding ask and frees its slot.
func (b *Binding) removeRequest(req *request) {
	delete(b.inflight, req.id)
	req.timer.Stop()
	req.slot.active = nil
}

// requestExpired fails the initial evaluations an unanswered ask carried, and
// ends a binding that reached the limit of consecutive retired asks.
// note: a timer that fires after the ask was answered finds it gone and does
// nothing.
// note: a retired re-evaluation is not queued again; the next probe asks it.
func (b *Binding) requestExpired(req *request) {
	c := b.c
	// why: the transport is closed outside the coordinator lock, so this
	// function unlocks by hand instead of deferring.
	c.mu.Lock()
	if b.inflight[req.id] != req {
		c.mu.Unlock()
		return
	}
	b.removeRequest(req)
	failAll(req.evaluations)
	b.timeouts++
	if b.unresponsive() {
		b.closeLocked()
		c.mu.Unlock()
		b.t.Close()
		return
	}
	b.schedule(req.slot)
	c.mu.Unlock()
}

// unresponsive reports whether the binding reached the limit of consecutive
// retired asks. A native binding never does.
func (b *Binding) unresponsive() bool {
	limit := b.c.cfg.MaxAskTimeouts
	return !b.native && limit > 0 && b.timeouts >= limit
}

// armProbe schedules the next probe of the binding's followed names, unless
// one is armed, the binding is native or closed, or probing is off.
func (b *Binding) armProbe() {
	interval := b.c.cfg.ProbeInterval
	if b.native || b.closed || interval <= 0 || b.probe != nil {
		return
	}
	b.probe = b.c.cfg.Clock.AfterFunc(interval, b.probeTick)
}

// probeTick re-asks the binding about every name a follow stream requests, so
// an unanswering provider accrues timeouts. It re-arms while followers remain.
// note: a slot with an active ask keeps the probe owed until that ask ends.
func (b *Binding) probeTick() {
	c := b.c
	c.mu.Lock()
	defer c.mu.Unlock()
	b.probe = nil
	if b.closed {
		return
	}
	if c.reevaluateFollowersOf(b, true) {
		b.armProbe()
	}
}

// stopProbe cancels an armed probe.
func (b *Binding) stopProbe() {
	if b.probe != nil {
		b.probe.Stop()
		b.probe = nil
	}
}

// sendLoop drains asks to the transport until the binding closes.
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

// failAll fails each evaluation in list.
// note: failing detaches an evaluation from list, so the loop runs on a copy.
func failAll(list []*evaluation) {
	for _, o := range append([]*evaluation(nil), list...) {
		o.fail()
	}
}

// endAll ends each evaluation in list.
// note: ending detaches an evaluation from list, so the loop runs on a copy.
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
