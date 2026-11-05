package nearby

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
)

// maxNoncesPerNode bounds the live nonces of one node; a status of a node at the bound is dropped.
//
// why: a flood of statuses under one key fills the share of that key only.
// note: a node sends about 4 statuses in statusTTL, and a scan reply at most once per 2 s per subnet.
const maxNoncesPerNode = 32

// maxNonces bounds the nonces of all nodes.
//
// why: every node the cache can hold keeps its full share, and the node being remembered still finds room.
const maxNonces = (maxCacheEntries + 1) * maxNoncesPerNode

// nonceMemory remembers the status nonces of each node, each until its status can no
// longer pass the freshness check.
//
// why: a mutex, not sig.Map: the bound checks and the insert are one step.
// why wall time: the freshness check compares the wire timestamp with the wall clock, so a
// nonce lifetime on the monotonic clock would end early after the wall clock steps back.
type nonceMemory struct {
	mu    sync.Mutex
	nodes map[string]map[astral.Nonce]time.Time // note: node, then nonce, to the end of the nonce's life
	count int
}

// seen reports whether node used nonce in a status that is still fresh at now.
func (m *nonceMemory) seen(node *astral.Identity, nonce astral.Nonce, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	until, ok := m.nodes[node.String()][nonce]
	return ok && !now.After(until)
}

// remember records that node used the nonce of proof. It fails when the nonce is still
// remembered, when node holds maxNoncesPerNode live nonces, or when the memory is full.
func (m *nonceMemory) remember(node *astral.Identity, proof *nearby.StatusProof, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := node.String()
	m.removeExpiredOf(key, now)
	nonces := m.nodes[key]
	if _, ok := nonces[proof.Nonce]; ok {
		return errReplay
	}
	if len(nonces) >= maxNoncesPerNode {
		return errNodeNonces
	}
	if m.count >= maxNonces {
		return errNonceMemoryFull
	}

	if nonces == nil {
		nonces = make(map[astral.Nonce]time.Time)
		if m.nodes == nil {
			m.nodes = make(map[string]map[astral.Nonce]time.Time)
		}
		m.nodes[key] = nonces
	}

	// why: a status passes the freshness check until statusTTL after its timestamp, which
	// may lie ahead of its arrival. The wire timestamp carries no monotonic reading.
	nonces[proof.Nonce] = proof.Timestamp.Time().Add(statusTTL)
	m.count++
	return nil
}

// retain forgets the nonces of every node not in keep, and the expired nonces at now.
func (m *nonceMemory) retain(keep map[string]bool, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, nonces := range m.nodes {
		if keep[key] {
			m.removeExpiredOf(key, now)
			continue
		}
		m.count -= len(nonces)
		delete(m.nodes, key)
	}
}

// removeExpired forgets the nonces whose status can no longer pass the freshness check at now.
func (m *nonceMemory) removeExpired(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key := range m.nodes {
		m.removeExpiredOf(key, now)
	}
}

// removeExpiredOf forgets the expired nonces of the node key; m.mu is held.
func (m *nonceMemory) removeExpiredOf(key string, now time.Time) {
	nonces := m.nodes[key]
	for nonce, until := range nonces {
		if now.After(until) {
			delete(nonces, nonce)
			m.count--
		}
	}
	if len(nonces) == 0 {
		delete(m.nodes, key)
	}
}

// size returns the number of nonces held.
func (m *nonceMemory) size() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.count
}
