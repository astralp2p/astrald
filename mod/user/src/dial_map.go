package user

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// The nearby contact limits per node. Spec: .ai/system/protocols/user/README.md
const (
	// dialInterval separates two contact attempts at one node.
	dialInterval = time.Minute
	// dialIntervalAfterRefusal separates an attempt whose contract check failed from the next.
	dialIntervalAfterRefusal = time.Hour
	// dialMapCap bounds the nodes the dial map holds.
	dialMapCap = 512
)

// dialMap holds, per node, the time of the next allowed nearby contact attempt.
//
// why a mutex and not sig.Map: reserve checks a node, prunes the map, makes room
// and records the node as one step.
type dialMap struct {
	mu   sync.Mutex
	next map[string]time.Time
}

func newDialMap() *dialMap {
	return &dialMap{next: make(map[string]time.Time)}
}

// reserve reports whether an attempt at node is allowed at now, and then records
// the next allowed attempt at now plus dialInterval. A full map makes room for a
// node it does not hold.
func (m *dialMap) reserve(node *astral.Identity, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := node.String()
	if next, ok := m.next[key]; ok && now.Before(next) {
		return false
	}

	m.prune(now)
	m.makeRoom(key)
	m.next[key] = now.Add(dialInterval)
	return true
}

// delay moves the next allowed attempt at node to until. A full map makes room
// for a node it does not hold.
func (m *dialMap) delay(node *astral.Identity, until time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := node.String()
	m.makeRoom(key)
	m.next[key] = until
}

// makeRoom removes the node whose next attempt comes first when the map is full
// and does not hold key; m.mu is held.
//
// why: the peer's contract check protects the node and the map only limits how
// often it dials, so unverified claims never lock out a new node of the user.
func (m *dialMap) makeRoom(key string) {
	if _, ok := m.next[key]; ok || len(m.next) < dialMapCap {
		return
	}

	var first string
	var firstNext time.Time
	for k, next := range m.next {
		if first == "" || next.Before(firstNext) {
			first, firstNext = k, next
		}
	}
	delete(m.next, first)
}

// prune removes the nodes whose next attempt is allowed at now.
func (m *dialMap) prune(now time.Time) {
	for key, next := range m.next {
		if !now.Before(next) {
			delete(m.next, key)
		}
	}
}
