package nearby

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// maxNonces bounds the remembered status nonces; a status is dropped while the memory is full.
//
// note: about 32 nonces per cached node at the cache cap; a node sends about 4 in statusTTL.
const maxNonces = 16384

// nonceKey names one status nonce of one node.
type nonceKey struct {
	node  string
	nonce astral.Nonce
}

// nonceMemory remembers the status nonces each node used, each until its status can no
// longer pass the freshness check.
//
// why: a mutex, not sig.Map: the cap check and the insert are one step.
type nonceMemory struct {
	mu    sync.Mutex
	until map[nonceKey]time.Time
}

// seen reports whether node used nonce in a status that is still fresh at now.
func (m *nonceMemory) seen(node *astral.Identity, nonce astral.Nonce, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	until, ok := m.until[nonceKey{node.String(), nonce}]
	return ok && !now.After(until)
}

// remember records that node used nonce in a status stamped ts, received at now. It
// fails when the nonce is still remembered or the memory is full.
func (m *nonceMemory) remember(node *astral.Identity, nonce astral.Nonce, ts, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := nonceKey{node.String(), nonce}
	if until, ok := m.until[key]; ok && !now.After(until) {
		return errReplay
	}
	if len(m.until) >= maxNonces {
		return errNonceMemoryFull
	}
	if m.until == nil {
		m.until = make(map[nonceKey]time.Time)
	}

	// why: a status passes the freshness check until statusTTL after its timestamp, which
	// may lie ahead of its arrival; a nonce kept only statusTTL after the arrival would
	// expire while its status is still fresh.
	m.until[key] = later(ts, now).Add(statusTTL)
	return nil
}

// removeExpired forgets the nonces whose status can no longer pass the freshness check at now.
func (m *nonceMemory) removeExpired(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for k, until := range m.until {
		if now.After(until) {
			delete(m.until, k)
		}
	}
}

// later returns the later of a and b.
func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
