package coordinator

import (
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// Contribution is one swarm member's contribution to a stream: the member's
// own discovery for the stream's caller, fed in by whoever reads it. Each
// method takes the coordinator lock; none blocks.
type Contribution struct {
	st          *Stream
	member      *astral.Identity
	unresolved  map[string]*evaluation // initial evaluation per service, until the member's outcome
	contributed map[offeringKey]bool   // offerings this member contributed, with the visibility tag each arrived with
	stale       map[offeringKey]bool   // after a restart: contributed offerings the member has not sent again; nil is none
	stop        func()                 // stops the carry; nil before it starts
	ended       bool                   // the member left, the stream closed, or a restart replaced it
}

func newContribution(st *Stream, member *astral.Identity) *Contribution {
	return &Contribution{st: st, member: member, unresolved: map[string]*evaluation{}, contributed: map[offeringKey]bool{}}
}

// Member is the swarm member this contribution comes from.
func (r *Contribution) Member() *astral.Identity { return r.member }

// Update applies one view from the member, which tagged its provider open or
// private. A view the caller may not see, as the node decided from that tag
// and the caller's permits, never reaches the caller and removes a view shown
// before.
func (r *Contribution) Update(u *services.Update, open, visible bool) {
	c := r.st.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.ended || r.st.closed || u == nil || u.ProviderID == nil || u.ProviderID.IsZero() {
		return
	}
	if !r.st.requests(string(u.Name)) {
		return
	}
	k := keyOf(u.ProviderID, string(u.Name))
	delete(r.stale, k)
	if !visible {
		if _, ours := r.contributed[k]; ours {
			delete(r.contributed, k)
			r.st.hide(k)
		}
		return
	}
	r.contributed[k] = open
	r.st.offer(k, u)
}

// Remove applies a removal the member sent.
func (r *Contribution) Remove(keys []*services.OfferingKey) {
	c := r.st.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.ended || r.st.closed {
		return
	}
	for _, ok := range keys {
		if ok == nil || ok.ProviderID == nil {
			continue
		}
		k := keyOf(ok.ProviderID, string(ok.Name))
		if _, ours := r.contributed[k]; !ours {
			continue
		}
		delete(r.contributed, k)
		delete(r.stale, k)
		// why: the cause names the hop this node saw, so the member's own
		// cause is not forwarded.
		r.st.retract(k, services.CauseMemberRemoved)
	}
	wake(r.st.wake)
}

// Initial records the member's initial outcome: the services it reported
// incomplete fail, the rest resolve. In a discovery of every service the
// member's one evaluation resolves and the services it reported are marked
// incomplete.
func (r *Contribution) Initial(incomplete []string) {
	c := r.st.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.ended {
		return
	}
	failed := map[string]struct{}{}
	for _, s := range incomplete {
		failed[s] = struct{}{}
	}
	if o := r.unresolved[allServices]; o != nil && r.st.all {
		for s := range failed {
			if s != allServices {
				o.at.incompleteNames[s] = struct{}{}
			}
		}
	}
	for _, o := range r.evaluations() {
		if _, f := failed[o.service]; f {
			o.fail()
		} else {
			o.end()
		}
	}
	r.sweepStaleLocked()
}

// Lost records that the member's contribution ended: unresolved initial work
// fails and every offering it contributed is retracted. The member may
// contribute again later.
func (r *Contribution) Lost() {
	c := r.st.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.ended {
		return
	}
	r.lostLocked(services.CauseMemberLost)
}

// lostLocked fails the member's unresolved initial work and retracts every
// offering it contributed with cause.
func (r *Contribution) lostLocked(cause services.RemovalCause) {
	for _, o := range r.evaluations() {
		o.fail()
	}
	if r.st.closed {
		return
	}
	for k := range r.contributed {
		r.st.retract(k, cause)
	}
	r.contributed = map[offeringKey]bool{}
	r.stale = nil
	wake(r.st.wake)
}

// evaluations lists the member's unresolved initial evaluations.
func (r *Contribution) evaluations() []*evaluation {
	list := make([]*evaluation, 0, len(r.unresolved))
	for _, o := range r.unresolved {
		list = append(list, o)
	}
	return list
}
