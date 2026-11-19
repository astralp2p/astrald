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

// RestartContributions replaces every live contribution of st with a fresh
// one and starts its carry, so each member sends its offerings anew. An
// offering the member sends again stays shown without a removal; one it does
// not send again before the new carry's initial outcome is removed.
func (c *Coordinator) RestartContributions(st *Stream) {
	c.mu.Lock()
	if st.closed {
		c.mu.Unlock()
		return
	}
	var restarted []*Contribution
	for i, r := range st.contributions {
		if r.ended {
			continue
		}
		next := r.replaceLocked()
		st.contributions[i] = next
		restarted = append(restarted, next)
	}
	c.mu.Unlock()

	c.startCarries(st, restarted)
}

// replaceLocked ends r without retracting anything and returns the
// contribution that takes over its offerings and unresolved initial work.
// why: an ended contribution ignores every later call, so a view or an
// initial outcome its old carry still delivers changes nothing; a carry of r
// that has not started yet is stopped as soon as it starts.
func (r *Contribution) replaceLocked() *Contribution {
	next := newContribution(r.st, r.member)
	next.contributed = r.contributed
	next.stale = maps.Clone(r.contributed)
	next.unresolved = r.unresolved
	for _, o := range next.unresolved {
		o.contribution = next
	}
	r.contributed = map[offeringKey]bool{}
	r.unresolved = map[string]*evaluation{}
	r.stale = nil
	r.endLocked()
	return next
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
