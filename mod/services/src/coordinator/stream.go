package coordinator

import (
	"bytes"
	"slices"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

// Sink writes one discovery stream to its consumer. Each call blocks until the
// object is written or the write fails; an error ends the stream.
type Sink interface {
	Update(*services.Update) error
	// TaggedUpdate writes a view of a tagged stream.
	TaggedUpdate(*services.TaggedUpdate) error
	Removed(*services.Removed) error
	// NotGranted writes the caller's current services.not_granted list after
	// an authority change altered it.
	NotGranted(*services.NotGranted) error
	// Boundary ends the initial attempt: Incomplete when incomplete is not nil,
	// then eos.
	Boundary(incomplete *services.Incomplete) error
}

// Stream is one consumer's discovery.
type Stream struct {
	c       *Coordinator
	caller  *astral.Identity
	names   map[string]struct{}
	all     bool // every service is requested
	follow  bool
	filter  *Visibility                      // nil passes every offering
	tagged  bool                             // views carry their provider's visibility
	written map[offeringKey]*services.Update // with tagged: the last available view written per offering

	visible           map[offeringKey]struct{}              // offerings shown as available
	unsent            map[offeringKey]*services.Update      // latest unsent view per offering
	latest            map[offeringKey]*services.Update      // latest available view queued or written per offering
	sendOrder         []offeringKey                         // send order of unsent keys
	unsentRemovals    map[offeringKey]services.RemovalCause // removals not yet sent, with their cause
	unsentNotGranted  *services.NotGranted                  // a not_granted list not yet sent; nil is none
	inflight          *offeringKey                          // view handed to the writer
	inflightAvailable bool                                  // the in-flight view offers the service
	attempt           *attempt                              // nil once the boundary is sent
	swarm             bool                                  // swarm members carry this discovery
	carrier           Carrier                               // nil starts no carry
	contributions     []*Contribution                       // swarm members carrying this discovery
	wake              chan struct{}
	done              chan struct{}
	closed            bool
}

func newStream(c *Coordinator, req Request) *Stream {
	st := &Stream{
		c:              c,
		caller:         req.Caller,
		names:          map[string]struct{}{},
		all:            req.All,
		follow:         req.Follow,
		filter:         req.Filter,
		tagged:         req.Tagged,
		swarm:          req.Swarm,
		carrier:        req.Carrier,
		written:        map[offeringKey]*services.Update{},
		visible:        map[offeringKey]struct{}{},
		unsent:         map[offeringKey]*services.Update{},
		latest:         map[offeringKey]*services.Update{},
		unsentRemovals: map[offeringKey]services.RemovalCause{},
		wake:           make(chan struct{}, 1),
		done:           make(chan struct{}),
	}
	for _, name := range req.Names {
		st.names[name] = struct{}{}
	}
	return st
}

// requests reports whether the stream discovers service.
func (st *Stream) requests(service string) bool {
	if st.all {
		return true
	}
	_, ok := st.names[service]
	return ok
}

// offerFrom queues a view a local binding answered, unless the caller may not
// see the binding's offering.
func (st *Stream) offerFrom(b *Binding, service string, k offeringKey, u *services.Update) {
	if st.filter != nil && !st.filter.Allows(service, st.c.isOpen(b, service)) {
		st.hide(k)
		return
	}
	st.offer(k, u)
}

// retag re-sends the last view of k written to a tagged stream, so the
// consumer reads k's current tag without waiting for the provider.
// note: a view queued or in flight needs nothing here; the writer reads the
// tag when it writes, and markWritten re-sends a view written with a stale tag.
func (st *Stream) retag(k offeringKey) {
	v := st.written[k]
	_, queued := st.unsent[k]
	inflight := st.inflight != nil && *st.inflight == k
	if v == nil || queued || inflight {
		return
	}
	st.offer(k, v)
}

// hide withholds k from a caller that may not see it: an unsent view is
// dropped, and a shown offering is removed.
func (st *Stream) hide(k offeringKey) {
	st.retract(k, services.CauseHidden)
	wake(st.wake)
}

// Contributions lists the swarm members carrying this discovery, in request
// order.
// why: membership changes replace the slice under the coordinator lock, so
// callers get a copy taken under it.
func (st *Stream) Contributions() []*Contribution {
	st.c.mu.Lock()
	defer st.c.mu.Unlock()
	return slices.Clone(st.contributions)
}

// Done is closed when the stream closes.
func (st *Stream) Done() <-chan struct{} { return st.done }

// shown reports whether the consumer has seen, or is being sent, an available
// view of k that no removal retracts.
func (st *Stream) shown(k offeringKey) bool {
	if st.inflight != nil && *st.inflight == k {
		return st.inflightAvailable
	}
	_, visible := st.visible[k]
	_, removing := st.unsentRemovals[k]
	return visible && !removing
}

// offer queues a view for the consumer. The latest unsent view replaces the
// older one in place; a negative view of an offering never shown is dropped.
func (st *Stream) offer(k offeringKey, u *services.Update) {
	if bool(u.Available) {
		st.latest[k] = u
	} else {
		delete(st.latest, k)
	}
	if bool(u.Available) || st.shown(k) {
		if _, queued := st.unsent[k]; !queued {
			st.sendOrder = append(st.sendOrder, k)
		}
		st.unsent[k] = u
	} else {
		st.dropUnsent(k)
	}
	wake(st.wake)
}

// dropUnsent removes an unsent view, so the boundary no longer waits for it.
func (st *Stream) dropUnsent(k offeringKey) {
	if _, queued := st.unsent[k]; queued {
		delete(st.unsent, k)
		for i, q := range st.sendOrder {
			if q == k {
				st.sendOrder = append(st.sendOrder[:i:i], st.sendOrder[i+1:]...)
				break
			}
		}
	}
	if st.attempt != nil && st.attempt.unsentAtBoundary != nil {
		delete(st.attempt.unsentAtBoundary, k)
	}
}

// repeats reports whether u is an available view equal to the latest one the
// consumer was sent or is about to be sent for k, with no removal pending.
func (st *Stream) repeats(k offeringKey, u *services.Update) bool {
	v := st.latest[k]
	if v == nil || !bool(u.Available) {
		return false
	}
	if _, removing := st.unsentRemovals[k]; removing {
		return false
	}
	return sameView(v, u)
}

// sameView reports whether two views encode to the same bytes.
func sameView(a, b *services.Update) bool {
	var x, y bytes.Buffer
	if _, err := a.WriteTo(&x); err != nil {
		return false
	}
	if _, err := b.WriteTo(&y); err != nil {
		return false
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}

// retractBinding purges a closed binding's views and queues a removal for
// those shown.
func (st *Stream) retractBinding(b *Binding) {
	if st.closed {
		return
	}
	for _, name := range b.names {
		if !st.requests(name) {
			continue
		}
		st.retract(keyOf(b.provider, name), services.CauseBindingClosed)
	}
	wake(st.wake)
}

// retract drops any unsent view of k and queues a removal with cause when the
// consumer was shown k as available.
// why: a key already queued keeps its first cause, as services.removed.md
// requires; shown alone does not ensure it while k's view is in flight.
func (st *Stream) retract(k offeringKey, cause services.RemovalCause) {
	shown := st.shown(k)
	delete(st.latest, k)
	st.dropUnsent(k)
	if _, queued := st.unsentRemovals[k]; shown && !queued {
		st.unsentRemovals[k] = cause
	}
}

// Close ends the stream on the consumer's side. Its initial evaluations leave
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
	ck := callerKey(st.caller)
	delete(st.c.byCaller[ck], st)
	if len(st.c.byCaller[ck]) == 0 {
		delete(st.c.byCaller, ck)
	}
	if at := st.attempt; at != nil {
		at.budget.Stop()
		for o := range at.unresolved {
			o.detach()
			if o.slot != nil {
				o.binding.removeIfIdle(o.slot)
			}
		}
	}
	for _, r := range st.contributions {
		r.endLocked()
	}
	st.unsent = map[offeringKey]*services.Update{}
	st.sendOrder = nil
	close(st.done)
	wake(st.wake)
}
