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
	for _, o := range append([]*evaluation(nil), req.evaluations...) {
		o.end()
	}
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
	for st := range b.c.byCaller[req.slot.caller.String()] {
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

	for _, caller := range b.audience(ch) {
		b.refresh(caller)
	}
	c.mu.Unlock()
	return nil
}

func (b *Binding) audience(ch *services.Change) []*Stream {
	var heads []*Stream
	if bool(ch.All) {
		for _, streams := range b.c.byCaller {
			for st := range streams {
				heads = append(heads, st)
				break
			}
		}
		return heads
	}
	for _, id := range ch.Callers {
		for st := range b.c.byCaller[id.String()] {
			heads = append(heads, st)
			break
		}
	}
	return heads
}

// refresh retains refresh work for every service of the source the stream's
// caller follows.
func (b *Binding) refresh(head *Stream) {
	ck := head.caller.String()
	var sl *askSlot
	for _, name := range b.names {
		if !b.c.followsService(ck, name) {
			continue
		}
		if sl == nil {
			sl = b.slot(head.caller)
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
	c := b.c
	b.closed = true
	close(b.done)

	for _, req := range b.inflight {
		req.timer.Stop()
		for _, o := range append([]*evaluation(nil), req.evaluations...) {
			o.fail()
		}
	}
	b.inflight = map[astral.Nonce]*request{}
	for _, sl := range b.slots {
		for _, obls := range sl.waiting {
			for _, o := range append([]*evaluation(nil), obls...) {
				o.fail()
			}
		}
	}
	b.slots = map[string]*askSlot{}

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
	for _, streams := range c.byCaller {
		for st := range streams {
			st.retractBinding(b)
		}
	}
}
