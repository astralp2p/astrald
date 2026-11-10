package coordinator

import (
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// Answer accepts a provider's answer. An answer for no outstanding ask is
// ignored. A malformed answer closes the source and returns ErrMalformed.
func (b *Binding) Answer(a *services.Answer) error {
	c := b.c
	c.mu.Lock()
	if b.closed {
		c.mu.Unlock()
		return nil
	}
	req := b.inflight[a.RequestID]
	if req == nil {
		c.mu.Unlock()
		return nil
	}
	if !b.isValidAnswer(req, a) {
		b.closeLocked()
		c.mu.Unlock()
		b.t.Close()
		return ErrMalformed
	}

	u := *a.Update
	u.ProviderID = b.provider
	b.removeRequest(req)
	b.deliver(req, &u)
	endAll(req.evaluations)
	b.schedule(req.slot)
	c.mu.Unlock()
	return nil
}

func (b *Binding) isValidAnswer(req *request, a *services.Answer) bool {
	switch {
	case a.Update == nil:
		return false
	case string(a.Update.Name) != req.service:
		return false
	case a.Update.ProviderID != nil && !a.Update.ProviderID.IsZero() && !a.Update.ProviderID.IsEqual(b.provider):
		return false
	}
	return true
}

// deliver offers an answer to the caller's streams requesting the service. A
// one-shot stream receives only answers to its own initial obligations.
func (b *Binding) deliver(req *request, u *services.Update) {
	k := keyOf(b.provider, req.service)
	for st := range b.c.byCaller[callerKey(req.slot.caller)] {
		if _, ok := st.names[req.service]; !ok || st.closed {
			continue
		}
		if st.follow || req.awaitedBy(st) {
			st.offer(k, u)
		}
	}
}

func (req *request) awaitedBy(st *Stream) bool {
	for _, o := range req.evaluations {
		if o.at.st == st {
			return true
		}
	}
	return false
}

// Change marks the selected callers' followed services for refresh. A change
// selecting no caller closes the source and returns ErrMalformed.
func (b *Binding) Change(ch *services.Change) error {
	c := b.c
	c.mu.Lock()
	if b.closed {
		c.mu.Unlock()
		return nil
	}
	if !ch.Valid() {
		b.closeLocked()
		c.mu.Unlock()
		b.t.Close()
		return ErrMalformed
	}

	for _, caller := range b.changedCallers(ch) {
		b.reevaluateFor(caller)
	}
	c.mu.Unlock()
	return nil
}

func (b *Binding) changedCallers(ch *services.Change) []*astral.Identity {
	var callers []*astral.Identity
	if bool(ch.All) {
		for _, streams := range b.c.byCaller {
			for st := range streams {
				callers = append(callers, st.caller)
				break
			}
		}
		return callers
	}
	for _, id := range ch.Callers {
		for st := range b.c.byCaller[callerKey(id)] {
			callers = append(callers, st.caller)
			break
		}
	}
	return callers
}

// refresh retains refresh work for every service of the source the stream's
// caller follows.
func (b *Binding) reevaluateFor(caller *astral.Identity) {
	var sl *askSlot
	for _, name := range b.names {
		if !b.c.follows(caller, name) {
			continue
		}
		if sl == nil {
			sl = b.slot(caller)
		}
		sl.reevaluate[name] = struct{}{}
	}
	if sl != nil {
		b.schedule(sl)
	}
}

// Close ends the source: pending asks retire, unresolved initial work fails,
// owned offerings are released, and streams that showed them get a removal.
// Close is idempotent.
func (b *Binding) Close() {
	c := b.c
	c.mu.Lock()
	if b.closed {
		c.mu.Unlock()
		return
	}
	b.closeLocked()
	c.mu.Unlock()
	b.t.Close()
}

func (b *Binding) closeLocked() {
	b.closed = true
	close(b.done)
	b.failOutstanding()
	b.releaseNames()
	b.retractFromStreams()
}

func (b *Binding) failOutstanding() {
	for _, req := range b.inflight {
		req.timer.Stop()
		failAll(req.evaluations)
	}
	b.inflight = map[astral.Nonce]*request{}
	for _, sl := range b.slots {
		for _, waiting := range sl.waiting {
			failAll(waiting)
		}
	}
	b.slots = map[string]*askSlot{}
}

func (b *Binding) releaseNames() {
	c := b.c
	for _, name := range b.names {
		k := keyOf(b.provider, name)
		if c.owners[k] == b {
			delete(c.owners, k)
		}
		delete(c.byName[name], b)
		if len(c.byName[name]) == 0 {
			delete(c.byName, name)
		}
	}
}

func (b *Binding) retractFromStreams() {
	for _, streams := range b.c.byCaller {
		for st := range streams {
			st.retractBinding(b)
		}
	}
}
