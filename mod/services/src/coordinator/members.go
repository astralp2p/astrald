package coordinator

import (
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// Carrier feeds a swarm member's contributions to the streams of one
// discovery.
type Carrier interface {
	// Carry starts feeding r and returns how to stop it. Carry runs outside
	// the coordinator lock.
	// note: stop runs under the coordinator lock; it must not block and must
	// not call back into the coordinator.
	Carry(r *Contribution) (stop func())
}

// SetMembers replaces the swarm member set. Every open swarm follow starts
// carrying to each member that joined, without initial work, and retracts
// and stops the contribution of each member that left.
func (c *Coordinator) SetMembers(members []*astral.Identity) {
	c.mu.Lock()
	next := map[string]struct{}{}
	for _, id := range members {
		next[id.String()] = struct{}{}
	}
	var joined []*astral.Identity
	for _, id := range members {
		if !c.isMember(id) {
			joined = append(joined, id)
		}
	}
	started := map[*Stream][]*Contribution{}
	for _, streams := range c.byCaller {
		for st := range streams {
			if !st.swarm || !st.follow || st.closed {
				continue
			}
			st.dropMembers(next)
			started[st] = st.addMembers(joined)
		}
	}
	c.members = append([]*astral.Identity(nil), members...)
	c.mu.Unlock()

	for st, rs := range started {
		c.startCarries(st, rs)
	}
}

// isMember reports whether id is in the member set.
func (c *Coordinator) isMember(id *astral.Identity) bool {
	for _, m := range c.members {
		if m.IsEqual(id) {
			return true
		}
	}
	return false
}

// addMembers adds one contribution per joined member. A joined member owes
// no initial evaluation.
// why: a late join never delays or marks incomplete an initial attempt that
// was already due.
func (st *Stream) addMembers(joined []*astral.Identity) []*Contribution {
	var added []*Contribution
	for _, id := range joined {
		r := newContribution(st, id)
		st.contributions = append(st.contributions, r)
		added = append(added, r)
	}
	return added
}

// dropMembers ends the contribution of every member not in keep: its
// unresolved initial work fails, its offerings are retracted, and its carry
// stops.
func (st *Stream) dropMembers(keep map[string]struct{}) {
	kept := st.contributions[:0:0]
	for _, r := range st.contributions {
		if _, ok := keep[r.member.String()]; ok {
			kept = append(kept, r)
			continue
		}
		r.lostLocked(services.CauseMemberLeft)
		r.endLocked()
	}
	st.contributions = kept
}

// startCarries starts the carry of each contribution. A contribution that
// ended while its carry started is stopped at once.
func (c *Coordinator) startCarries(st *Stream, rs []*Contribution) {
	if st.carrier == nil {
		return
	}
	for _, r := range rs {
		stop := st.carrier.Carry(r)
		c.mu.Lock()
		ended := r.ended
		if !ended {
			r.stop = stop
		}
		c.mu.Unlock()
		if ended {
			stop()
		}
	}
}

// endLocked marks the contribution ended and stops its carry. Later calls
// from the carry change nothing.
func (r *Contribution) endLocked() {
	if r.ended {
		return
	}
	r.ended = true
	if r.stop != nil {
		r.stop()
		r.stop = nil
	}
}
