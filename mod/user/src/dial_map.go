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
// why a mutex and not sig.Map: reserve checks a node, prunes the map, checks the
// cap and records the node as one step.
type dialMap struct {
	mu   sync.Mutex
	next map[string]time.Time
}

func newDialMap() *dialMap {
	return &dialMap{next: make(map[string]time.Time)}
}

// reserve reports whether an attempt at node is allowed at now, and then records
// the next allowed attempt at now plus dialInterval. A full map refuses a node
// it does not hold.
func (m *dialMap) reserve(node *astral.Identity, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := node.String()
	if next, ok := m.next[key]; ok && now.Before(next) {
		return false
	}

	m.prune(now)
	if _, ok := m.next[key]; !ok && len(m.next) >= dialMapCap {
		return false
	}

	m.next[key] = now.Add(dialInterval)
	return true
}

// delay moves the next allowed attempt at node to until. A full map that does
// not hold node leaves it out.
func (m *dialMap) delay(node *astral.Identity, until time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := node.String()
	if _, ok := m.next[key]; ok || len(m.next) < dialMapCap {
		m.next[key] = until
	}
}

// prune removes the nodes whose next attempt is allowed at now.
func (m *dialMap) prune(now time.Time) {
	for key, next := range m.next {
		if !now.Before(next) {
			delete(m.next, key)
		}
	}
}
