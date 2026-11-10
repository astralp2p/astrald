package coordinator

import (
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// Remote is one swarm member's contribution to a stream: the member's own
// discovery for the stream's caller, fed in by whoever reads it. Each method
// takes the coordinator lock; none blocks.
type Contribution struct {
	st          *Stream
	member      *astral.Identity
	unresolved  map[string]*evaluation   // initial obligation per service, until the member's outcome
	contributed map[offeringKey]struct{} // offerings this member contributed
}

func newContribution(st *Stream, member *astral.Identity) *Contribution {
	return &Contribution{st: st, member: member, unresolved: map[string]*evaluation{}, contributed: map[offeringKey]struct{}{}}
}

// Member is the swarm member this contribution comes from.
func (r *Contribution) Member() *astral.Identity { return r.member }

// Offer applies one view from the member.
func (r *Contribution) Update(u *services.Update) {
	c := r.st.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.st.closed || u == nil || u.ProviderID == nil || u.ProviderID.IsZero() {
		return
	}
	if _, requested := r.st.names[string(u.Name)]; !requested {
		return
	}
	k := keyOf(u.ProviderID, string(u.Name))
	r.contributed[k] = struct{}{}
	r.st.offer(k, u)
}

// Remove applies a removal the member sent.
func (r *Contribution) Remove(keys []*services.OfferingKey) {
	c := r.st.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.st.closed {
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
		r.st.retract(k)
	}
	wake(r.st.wake)
}

// Initial records the member's initial outcome: the services it reported
// incomplete fail, the rest resolve.
func (r *Contribution) Initial(incomplete []string) {
	c := r.st.c
	c.mu.Lock()
	defer c.mu.Unlock()
	failed := map[string]struct{}{}
	for _, s := range incomplete {
		failed[s] = struct{}{}
	}
	for _, o := range r.evaluations() {
		if _, f := failed[o.service]; f {
			o.fail()
		} else {
			o.end()
		}
	}
}

// Lost records that the member's contribution ended: unresolved initial work
// fails and every offering it contributed is retracted. The member may
// contribute again later.
func (r *Contribution) Lost() {
	c := r.st.c
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, o := range r.evaluations() {
		o.fail()
	}
	if r.st.closed {
		return
	}
	for k := range r.contributed {
		r.st.retract(k)
	}
	r.contributed = map[offeringKey]struct{}{}
	wake(r.st.wake)
}

func (r *Contribution) evaluations() []*evaluation {
	list := make([]*evaluation, 0, len(r.unresolved))
	for _, o := range r.unresolved {
		list = append(list, o)
	}
	return list
}
