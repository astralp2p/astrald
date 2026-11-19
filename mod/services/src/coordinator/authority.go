package coordinator

import (
	"maps"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// The node re-checks an open follow when its caller's authority changes and
// applies the result through these methods. Authorization runs before each
// call, outside the coordinator lock.
//
// Spec: .ai/system/protocols/services/ops/services.discover.md#authority-changes

// SetFilter replaces the visibility filter of st. A local offering st may no
// longer see is withheld, a queued view included; on a follow, a binding st
// may now see is evaluated again. A non-nil notGranted is queued for the
// consumer as the caller's current list.
func (c *Coordinator) SetFilter(st *Stream, f *Visibility, notGranted *services.NotGranted) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st.closed {
		return
	}
	old := st.filter
	st.filter = f
	if notGranted != nil {
		st.unsentNotGranted = notGranted
		wake(st.wake)
	}
	for name, bindings := range c.byName {
		if !st.requests(name) {
			continue
		}
		for b := range bindings {
			open := c.isOpen(b, name)
			switch {
			case f != nil && !f.Allows(name, open):
				st.hide(keyOf(b.provider, name))
			case st.follow && old != nil && !old.Allows(name, open):
				sl := b.slot(st.caller)
				sl.reevaluate[name] = struct{}{}
				b.schedule(sl)
			}
		}
	}
}

// Offering is one offering a swarm member contributed to a stream, with the
// visibility tag it arrived with.
type Offering struct {
	ProviderID *astral.Identity
	Name       string
	Open       bool
}

// Offered lists the offerings the member contributed and the stream still
// holds.
func (r *Contribution) Offered() []Offering {
	c := r.st.c
	c.mu.Lock()
	defer c.mu.Unlock()
	list := make([]Offering, 0, len(r.contributed))
	for k, open := range r.contributed {
		id, err := astral.ParseIdentity(k.provider)
		if err != nil {
			continue
		}
		list = append(list, Offering{ProviderID: id, Name: k.name, Open: open})
	}
	return list
}

// Hide withholds offerings of the member the caller may no longer see: a
// queued view is dropped and a shown one is removed with cause hidden. An
// offering the member no longer contributes is skipped.
func (r *Contribution) Hide(list []Offering) {
	c := r.st.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.ended || r.st.closed {
		return
	}
	for _, o := range list {
		k := keyOf(o.ProviderID, o.Name)
		if _, ours := r.contributed[k]; !ours {
			continue
		}
		delete(r.contributed, k)
		delete(r.stale, k)
		r.st.hide(k)
	}
}

// RestartContributions stops the carry of every live contribution of st and
// starts it again, so each member sends its offerings anew. An offering the
// member sends again stays shown without a removal; one it does not send again
// before its next initial outcome is removed.
func (c *Coordinator) RestartContributions(st *Stream) {
	c.mu.Lock()
	if st.closed {
		c.mu.Unlock()
		return
	}
	var live []*Contribution
	for _, r := range st.contributions {
		if r.ended {
			continue
		}
		// why: a stopped carry returns without Lost, so the restart retracts
		// nothing the member still offers.
		if r.stop != nil {
			r.stop()
			r.stop = nil
		}
		r.stale = maps.Clone(r.contributed)
		live = append(live, r)
	}
	c.mu.Unlock()

	c.startCarries(st, live)
}

// sweepStaleLocked removes the offerings a restarted member did not send
// again before its initial outcome.
func (r *Contribution) sweepStaleLocked() {
	if r.stale == nil || r.st.closed {
		r.stale = nil
		return
	}
	for k := range r.stale {
		delete(r.contributed, k)
		r.st.retract(k, services.CauseMemberRemoved)
	}
	r.stale = nil
	wake(r.st.wake)
}
