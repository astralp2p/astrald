package mcp

import (
	"sync"

	"github.com/astralp2p/astral-go/astral"
)

// waiters is the set of parked agents, keyed by the identity whose mailbox each
// one is watching. Its zero value is ready to use.
//
// why a wake carries nothing: what a waiter answers is decided by its own
// query, and the writer knows none of it. The correspondent filter is a name
// the waiter resolved in its own call, the cursor is the client's and this node
// keeps no copy, and archived_at moves in both directions long after a row
// lands — Unarchive puts a message back into the wait set with no insert at
// all. A wake says look again; the query says what is there.
//
// why that also makes coalescing free: two arrivals collapsing into one wake
// lose nothing, because the query that follows sees both.
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
// why it is keyed by owner rather than broadcast: measured over two hundred
// parked agents, one arrival for one of them costs two hundred queries
// broadcast and one keyed. The gap grows with how many agents are parked, which
// is the case a wake exists to serve, so a broadcast is worse the more it is
// needed.
//
// why the send never blocks: the caller is answering a delivery and still owes
// its sender an Ack. A wake that blocked would hold that ack until the sender's
// query timeout closed the conn, and the sender would then record a message
// that is in fact stored as one whose fate is unknown.
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
