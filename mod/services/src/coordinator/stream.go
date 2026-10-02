package coordinator

import (
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// Sink writes one discovery stream to its consumer. Each call blocks until the
// object is written or the write fails; an error ends the stream.
type Sink interface {
	Update(*services.Update) error
	Removed(*services.Removed) error
	// Boundary ends the initial attempt: Incomplete when incomplete is not nil,
	// then eos.
	Boundary(incomplete *services.Incomplete) error
}

// Stream is one consumer discovery.
type Stream struct {
	c        *Coordinator
	caller   *astral.Identity
	services map[string]struct{}
	follow   bool

	visible           map[offeringKey]struct{}         // offerings shown as available
	pending           map[offeringKey]*services.Update // T6: latest unsent view per offering
	queue             []offeringKey                    // send order of pending keys
	removed           map[offeringKey]struct{}         // removals not yet sent
	inflight          *offeringKey                     // view handed to the writer
	inflightAvailable bool                             // the in-flight view offers the service
	attempt           *attempt                         // nil once the boundary is sent
	remotes           []*Remote                        // swarm members carrying this discovery
	wake              chan struct{}
	done              chan struct{}
	closed            bool
}

func newStream(c *Coordinator, caller *astral.Identity, names []string, follow bool) *Stream {
	st := &Stream{
		c:        c,
		caller:   caller,
		services: map[string]struct{}{},
		follow:   follow,
		visible:  map[offeringKey]struct{}{},
		pending:  map[offeringKey]*services.Update{},
		removed:  map[offeringKey]struct{}{},
		wake:     make(chan struct{}, 1),
		done:     make(chan struct{}),
	}
	for _, name := range names {
		st.services[name] = struct{}{}
	}
	return st
}

// Remotes are the swarm members carrying this discovery, in request order.
func (st *Stream) Remotes() []*Remote { return st.remotes }

// Done is closed when the stream closes.
func (st *Stream) Done() <-chan struct{} { return st.done }

// shown reports whether the consumer has seen, or is being sent, an available
// view of k that no removal retracts.
func (st *Stream) shown(k offeringKey) bool {
	if st.inflight != nil && *st.inflight == k {
		return st.inflightAvailable
	}
	_, visible := st.visible[k]
	_, removing := st.removed[k]
	return visible && !removing
}

// offer applies T6 replacement and never-introduced negative suppression.
func (st *Stream) offer(k offeringKey, u *services.Update) {
	if bool(u.Available) || st.shown(k) {
		if _, queued := st.pending[k]; !queued {
			st.queue = append(st.queue, k)
		}
		st.pending[k] = u
	} else {
		st.drop(k)
	}
	wake(st.wake)
}

// drop removes an unsent view and releases it from the boundary's obligations.
func (st *Stream) drop(k offeringKey) {
	if _, queued := st.pending[k]; queued {
		delete(st.pending, k)
		for i, q := range st.queue {
			if q == k {
				st.queue = append(st.queue[:i:i], st.queue[i+1:]...)
				break
			}
		}
	}
	if st.attempt != nil && st.attempt.owed != nil {
		delete(st.attempt.owed, k)
	}
}

// lose purges a closed source's views and queues a removal for those shown.
func (st *Stream) lose(src *Source) {
	if st.closed {
		return
	}
	for _, name := range src.names {
		if _, ok := st.services[name]; !ok {
			continue
		}
		st.retract(keyOf(src.provider, name))
	}
	wake(st.wake)
}

// retract drops any unsent view of k and queues a removal when the consumer
// was shown k as available.
func (st *Stream) retract(k offeringKey) {
	shown := st.shown(k)
	st.drop(k)
	if shown {
		st.removed[k] = struct{}{}
	}
}

// Close ends the stream on the consumer's side. Its initial obligations leave
// their slots without failing anything. Close is idempotent.
func (st *Stream) Close() {
	st.c.mu.Lock()
	defer st.c.mu.Unlock()
	st.closeLocked()
}

func (st *Stream) closeLocked() {
	if st.closed {
		return
	}
	st.closed = true
	ck := st.caller.String()
	delete(st.c.byCaller[ck], st)
	if len(st.c.byCaller[ck]) == 0 {
		delete(st.c.byCaller, ck)
	}
	if at := st.attempt; at != nil {
		at.budget.Stop()
		for o := range at.open {
			o.detach()
			if o.slot != nil {
				o.src.collect(o.slot)
			}
		}
	}
	st.pending = map[offeringKey]*services.Update{}
	st.queue = nil
	close(st.done)
	wake(st.wake)
}
