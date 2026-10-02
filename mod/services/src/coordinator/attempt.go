package coordinator

import (
	"sort"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// attempt is the initial discovery of one stream.
type attempt struct {
	st       *Stream
	open     map[*obligation]struct{} // initial evaluations not yet ended
	failed   map[string]struct{}      // services with unresolved or failed work
	resolved bool                     // open emptied, or the budget expired
	owed     map[offeringKey]struct{} // views the boundary waits for
	budget   Timer
}

// obligation is one initial evaluation an attempt waits for. It sits in
// exactly one place: its slot's initial list while waiting, or req.obls once
// asked.
type obligation struct {
	at      *attempt
	src     *Source
	slot    *slot
	service string
	req     *request
	remote  *Remote // set for a swarm member's contribution; src and slot are nil
}

func newAttempt(st *Stream) *attempt {
	return &attempt{st: st, open: map[*obligation]struct{}{}, failed: map[string]struct{}{}}
}

// detach removes o from its container and from the attempt.
func (o *obligation) detach() {
	switch {
	case o.remote != nil:
		delete(o.remote.open, o.service)
	case o.req != nil:
		o.req.obls = removeObligation(o.req.obls, o)
	default:
		list := removeObligation(o.slot.initial[o.service], o)
		if len(list) == 0 {
			delete(o.slot.initial, o.service)
		} else {
			o.slot.initial[o.service] = list
		}
	}
	delete(o.at.open, o)
}

// end closes o; the last one to end finishes the attempt.
func (o *obligation) end() {
	if _, open := o.at.open[o]; !open {
		return
	}
	o.detach()
	if len(o.at.open) == 0 && !o.at.resolved {
		o.at.finish()
	}
}

func (o *obligation) fail() {
	if _, open := o.at.open[o]; !open {
		return
	}
	o.at.failed[o.service] = struct{}{}
	o.end()
}

func removeObligation(list []*obligation, o *obligation) []*obligation {
	for i, x := range list {
		if x == o {
			return append(list[:i:i], list[i+1:]...)
		}
	}
	return list
}

// finish freezes what the boundary owes: the views pending or in flight now.
func (at *attempt) finish() {
	at.resolved = true
	if at.budget != nil {
		at.budget.Stop()
	}
	st := at.st
	at.owed = map[offeringKey]struct{}{}
	for _, k := range st.queue {
		at.owed[k] = struct{}{}
	}
	if st.inflight != nil {
		at.owed[*st.inflight] = struct{}{}
	}
	wake(st.wake)
}

func (at *attempt) incomplete() *services.Incomplete {
	if len(at.failed) == 0 {
		return nil
	}
	names := make([]string, 0, len(at.failed))
	for name := range at.failed {
		names = append(names, name)
	}
	sort.Strings(names)
	inc := &services.Incomplete{}
	for _, name := range names {
		inc.Services = append(inc.Services, astral.String8(name))
	}
	return inc
}

func (c *Coordinator) budgetExpired(at *attempt) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if at.resolved || at.st.closed {
		return
	}
	for o := range at.open {
		at.failed[o.service] = struct{}{}
		o.detach()
		if o.slot != nil {
			o.src.collect(o.slot)
		}
	}
	at.finish()
}
