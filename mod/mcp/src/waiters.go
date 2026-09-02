package mcp

import (
	"sync"

	"github.com/astralp2p/astral-go/astral"
)

// waiters is the set of parked agents, keyed by the identity whose mailbox each
// one is watching. Its zero value is ready to use.
//
// why a wake carries nothing: what a waiter answers is decided by its own query
// — a correspondent it resolved, a cursor this node keeps no copy of, an
// archived_at that moves in both directions. A wake says look again; the query
// says what is there, which also makes coalescing free.
type waiters struct {
	mu sync.Mutex
	m  map[string]map[chan struct{}]struct{}
}

// park registers a waiter and answers the channel it will be woken on together
// with the function that removes it. The caller must run that function on every
// exit, or the set grows by one entry per park.
//
// why the channel is buffered to one: a wake is an edge and the answer is a
// level, so a token held while the waiter is between statements is caught
// rather than dropped, and a second token would say nothing the first does not.
func (w *waiters) park(owner *astral.Identity) (woke <-chan struct{}, leave func()) {
	ch := make(chan struct{}, 1)
	key := owner.String()

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.m == nil {
		w.m = map[string]map[chan struct{}]struct{}{}
	}
	if w.m[key] == nil {
		w.m[key] = map[chan struct{}]struct{}{}
	}

	w.m[key][ch] = struct{}{}

	return ch, func() {
		w.mu.Lock()
		defer w.mu.Unlock()

		delete(w.m[key], ch)
		if len(w.m[key]) == 0 {
			delete(w.m, key)
		}
	}
}

// wake tells this owner's parked waiters to look again. An owner with none is
// the common case and costs one map lookup.
//
// why it is keyed by owner rather than broadcast: a broadcast costs one query
// per parked agent for every arrival.
//
// why the send never blocks: the caller still owes its sender an Ack, and
// holding it until the query timeout makes a stored message read as one whose
// fate is unknown.
func (w *waiters) wake(owner *astral.Identity) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for ch := range w.m[owner.String()] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// parked answers how many waiters this owner has. It exists for the tests: a
// registration that is never removed is a leak with no other symptom.
func (w *waiters) parked(owner *astral.Identity) int {
	w.mu.Lock()
	defer w.mu.Unlock()

	return len(w.m[owner.String()])
}
