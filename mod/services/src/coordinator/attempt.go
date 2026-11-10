package coordinator

import (
	"sort"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// attempt is the initial attempt of one stream: the work that ends at its
// boundary.
type attempt struct {
	st               *Stream
	unresolved       map[*evaluation]struct{} // initial evaluations not yet ended
	incompleteNames  map[string]struct{}      // services with unresolved or failed work
	resolved         bool                     // unresolved emptied, or the budget expired
	unsentAtBoundary map[offeringKey]struct{} // views the boundary waits for
	budget           Timer
}

// evaluation is one initial evaluation an attempt waits for. It sits in
// exactly one place: its slot's waiting list, its request once asked, or its
// contribution for a swarm member.
type evaluation struct {
	at           *attempt
	binding      *Binding
	slot         *askSlot
	service      string
	req          *request
	contribution *Contribution // set for a swarm member; binding and slot are nil
}

func newAttempt(st *Stream) *attempt {
	return &attempt{st: st, unresolved: map[*evaluation]struct{}{}, incompleteNames: map[string]struct{}{}}
}

// detach removes o from its container and from the attempt.
func (o *evaluation) detach() {
	switch {
	case o.contribution != nil:
		delete(o.contribution.unresolved, o.service)
	case o.req != nil:
		o.req.evaluations = removeEvaluation(o.req.evaluations, o)
	default:
		list := removeEvaluation(o.slot.waiting[o.service], o)
		if len(list) == 0 {
			delete(o.slot.waiting, o.service)
		} else {
			o.slot.waiting[o.service] = list
		}
	}
	delete(o.at.unresolved, o)
}

// end closes o; the last one to end finishes the attempt.
func (o *evaluation) end() {
	if _, open := o.at.unresolved[o]; !open {
		return
	}
	o.detach()
	if len(o.at.unresolved) == 0 && !o.at.resolved {
		o.at.finish()
	}
}

// fail ends o and reports its service incomplete.
func (o *evaluation) fail() {
	if _, open := o.at.unresolved[o]; !open {
		return
	}
	o.at.incompleteNames[o.service] = struct{}{}
	o.end()
}

func removeEvaluation(list []*evaluation, o *evaluation) []*evaluation {
	for i, x := range list {
		if x == o {
			return append(list[:i:i], list[i+1:]...)
		}
	}
	return list
}

// finish freezes what the boundary waits for: the views unsent or in flight
// now.
func (at *attempt) finish() {
	at.resolved = true
	if at.budget != nil {
		at.budget.Stop()
	}
	st := at.st
	at.unsentAtBoundary = map[offeringKey]struct{}{}
	for _, k := range st.sendOrder {
		at.unsentAtBoundary[k] = struct{}{}
	}
	if st.inflight != nil {
		at.unsentAtBoundary[*st.inflight] = struct{}{}
	}
	wake(st.wake)
}

// incomplete is the services.incomplete the boundary sends, or nil.
func (at *attempt) incomplete() *services.Incomplete {
	if len(at.incompleteNames) == 0 {
		return nil
	}
	// why: a member lost in a discovery of every service failed work no name
	// describes; the boundary still reports it, with an empty list.
	names := make([]string, 0, len(at.incompleteNames))
	for name := range at.incompleteNames {
		if name != allServices {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	inc := &services.Incomplete{}
	for _, name := range names {
		inc.Services = append(inc.Services, astral.String8(name))
	}
	return inc
}

// budgetExpired fails every initial evaluation still unresolved and ends the
// attempt.
func (c *Coordinator) budgetExpired(at *attempt) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if at.resolved || at.st.closed {
		return
	}
	for o := range at.unresolved {
		at.incompleteNames[o.service] = struct{}{}
		o.detach()
		if o.slot != nil {
			o.binding.removeIfIdle(o.slot)
		}
	}
	at.finish()
}
