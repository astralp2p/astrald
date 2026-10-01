package coordinator

import (
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// Answer accepts a provider's answer. An answer for no outstanding ask is
// ignored. A malformed answer closes the source and returns ErrMalformed.
func (src *Source) Answer(a *services.Answer) error {
	c := src.c
	c.mu.Lock()
	if src.closed {
		c.mu.Unlock()
		return nil
	}
	req := src.pending[a.RequestID]
	if req == nil {
		c.mu.Unlock()
		return nil
	}
	if !src.acceptable(req, a) {
		src.closeLocked()
		c.mu.Unlock()
		src.t.Close()
		return ErrMalformed
	}

	u := *a.Update
	u.ProviderID = src.provider
	src.retire(req)
	src.deliver(req, &u)
	for _, o := range append([]*obligation(nil), req.obls...) {
		o.end()
	}
	src.schedule(req.slot)
	c.mu.Unlock()
	return nil
}

func (src *Source) acceptable(req *request, a *services.Answer) bool {
	switch {
	case a.Update == nil:
		return false
	case string(a.Update.Name) != req.service:
		return false
	case a.Update.ProviderID != nil && !a.Update.ProviderID.IsZero() && !a.Update.ProviderID.IsEqual(src.provider):
		return false
	}
	return true
}

// deliver offers an answer to the caller's streams requesting the service. A
// one-shot stream receives only answers to its own initial obligations.
func (src *Source) deliver(req *request, u *services.Update) {
	k := keyOf(src.provider, req.service)
	for st := range src.c.byCaller[req.slot.caller.String()] {
		if _, ok := st.services[req.service]; !ok || st.closed {
			continue
		}
		if st.follow || req.owes(st) {
			st.offer(k, u)
		}
	}
}

func (req *request) owes(st *Stream) bool {
	for _, o := range req.obls {
		if o.at.st == st {
			return true
		}
	}
	return false
}

// Change marks the selected callers' followed services for refresh. A change
// selecting no caller closes the source and returns ErrMalformed.
func (src *Source) Change(ch *services.Change) error {
	c := src.c
	c.mu.Lock()
	if src.closed {
		c.mu.Unlock()
		return nil
	}
	if !ch.Valid() {
		src.closeLocked()
		c.mu.Unlock()
		src.t.Close()
		return ErrMalformed
	}

	for _, caller := range src.audience(ch) {
		src.refresh(caller)
	}
	c.mu.Unlock()
	return nil
}

func (src *Source) audience(ch *services.Change) []*Stream {
	var heads []*Stream
	if bool(ch.All) {
		for _, streams := range src.c.byCaller {
			for st := range streams {
				heads = append(heads, st)
				break
			}
		}
		return heads
	}
	for _, id := range ch.Callers {
		for st := range src.c.byCaller[id.String()] {
			heads = append(heads, st)
			break
		}
	}
	return heads
}

// refresh retains refresh work for every service of the source the stream's
// caller follows.
func (src *Source) refresh(head *Stream) {
	ck := head.caller.String()
	var sl *slot
	for _, name := range src.names {
		if !src.c.followsService(ck, name) {
			continue
		}
		if sl == nil {
			sl = src.slot(head.caller)
		}
		sl.dirty[name] = struct{}{}
	}
	if sl != nil {
		src.schedule(sl)
	}
}

// Close ends the source: pending asks retire, unresolved initial work fails,
// owned offerings are released, and streams that showed them get a removal.
// Close is idempotent.
func (src *Source) Close() {
	c := src.c
	c.mu.Lock()
	if src.closed {
		c.mu.Unlock()
		return
	}
	src.closeLocked()
	c.mu.Unlock()
	src.t.Close()
}

func (src *Source) closeLocked() {
	c := src.c
	src.closed = true
	close(src.done)

	for _, req := range src.pending {
		req.timer.Stop()
		for _, o := range append([]*obligation(nil), req.obls...) {
			o.fail()
		}
	}
	src.pending = map[astral.Nonce]*request{}
	for _, sl := range src.slots {
		for _, obls := range sl.initial {
			for _, o := range append([]*obligation(nil), obls...) {
				o.fail()
			}
		}
	}
	src.slots = map[string]*slot{}

	for _, name := range src.names {
		k := keyOf(src.provider, name)
		if c.owners[k] == src {
			delete(c.owners, k)
		}
		delete(c.byName[name], src)
		if len(c.byName[name]) == 0 {
			delete(c.byName, name)
		}
	}
	for _, streams := range c.byCaller {
		for st := range streams {
			st.lose(src)
		}
	}
}
