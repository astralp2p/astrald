package nearby

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
)

// fillNonces fills m to maxNonces with the nonces of nodes no cache holds, each live until until.
func fillNonces(m *nonceMemory, until time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.nodes == nil {
		m.nodes = make(map[string]map[astral.Nonce]time.Time)
	}
	for i := 0; m.count < maxNonces; i++ {
		nonces := make(map[astral.Nonce]time.Time)
		for j := 0; j < maxNoncesPerNode && m.count < maxNonces; j++ {
			nonces[astral.Nonce(j)] = until
			m.count++
		}
		m.nodes[fmt.Sprintf("uncached-%d", i)] = nonces
	}
}

// A flood of statuses under one key fills that key's share of the nonce memory only: the
// flooder is refused at its bound, and another node is still verified and reported.
func TestNonceFloodUnderOneKeyLeavesOtherNodes(t *testing.T) {
	h := newHarness(t, nil)
	flooder, sibling := newPeer(t), newPeer(t)
	ahead := h.clock.now().Add(statusTTL)

	for i := range maxNoncesPerNode {
		if err := h.mod.acceptStatus(ipOf("192.168.1.66"), flooder.visible(t, ahead, nil, "")); err != nil {
			t.Fatalf("flood status %d: acceptStatus = %v; want nil", i, err)
		}
	}
	for range 3 {
		err := h.mod.acceptStatus(ipOf("192.168.1.66"), flooder.visible(t, ahead, nil, ""))
		if !errors.Is(err, errNodeNonces) {
			t.Fatalf("flood status over the bound: acceptStatus = %v; want %v", err, errNodeNonces)
		}
	}
	if n := h.mod.nonces.size(); n != maxNoncesPerNode {
		t.Fatalf("nonce memory holds %d nonces; want the flooder's %d", n, maxNoncesPerNode)
	}

	if err := h.mod.acceptStatus(ipOf("192.168.1.10"), sibling.visible(t, h.clock.now(), nil, "")); err != nil {
		t.Fatalf("other node after the flood: acceptStatus = %v; want nil", err)
	}
	seen := h.events.peersSeen(t)
	if len(seen) != 2 || !seen[1].NodeID.IsEqual(sibling.node) {
		t.Fatalf("PeerSeen = %+v; want the flooder's and then the other node's", seen)
	}
}

// A node at its bound gets a slot again once one of its statuses is no longer fresh.
func TestNonceSlotFreesWhenItsStatusGoesStale(t *testing.T) {
	h := newHarness(t, nil)
	p := newPeer(t)
	start := h.clock.now()

	for range maxNoncesPerNode {
		if err := h.mod.acceptStatus(ipOf(peerIP), p.visible(t, start, nil, "")); err != nil {
			t.Fatalf("acceptStatus: %v", err)
		}
	}

	now := h.clock.advance(statusTTL)
	if err := h.mod.acceptStatus(ipOf(peerIP), p.visible(t, now, nil, "")); !errors.Is(err, errNodeNonces) {
		t.Fatalf("at the end of the first statuses' life: acceptStatus = %v; want %v", err, errNodeNonces)
	}
	now = h.clock.advance(time.Nanosecond)
	if err := h.mod.acceptStatus(ipOf(peerIP), p.visible(t, now, nil, "")); err != nil {
		t.Fatalf("after the first statuses went stale: acceptStatus = %v; want nil", err)
	}
}

// A full nonce memory forgets the nodes the cache does not hold and keeps the cached ones,
// so a new node is verified and a cached node's status still cannot be replayed.
func TestFullNonceMemoryForgetsUncachedNodes(t *testing.T) {
	h := newHarness(t, nil)
	cached, newcomer := newPeer(t), newPeer(t)
	first := cached.visible(t, h.clock.now(), nil, "")
	if err := h.mod.acceptStatus(ipOf(peerIP), first); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}
	fillNonces(&h.mod.nonces, h.clock.now().Add(statusTTL))

	if err := h.mod.acceptStatus(ipOf("192.168.1.21"), newcomer.visible(t, h.clock.now(), nil, "")); err != nil {
		t.Fatalf("new node with a full nonce memory: acceptStatus = %v; want nil", err)
	}
	if n := h.mod.nonces.size(); n != 2 {
		t.Fatalf("nonce memory holds %d nonces; want the cached node's and the new node's", n)
	}
	if err := h.mod.acceptStatus(ipOf("192.168.1.22"), first); !errors.Is(err, errReplay) {
		t.Fatalf("replay of the cached node: acceptStatus = %v; want %v", err, errReplay)
	}
	if seen := h.events.peersSeen(t); len(seen) != 2 {
		t.Fatalf("emitted %d PeerSeen; want 2", len(seen))
	}
}

// A full nonce memory keeps the nonces of the node it remembers, also when the cache holds
// no entry of that node.
func TestFullNonceMemoryKeepsTheRememberedNode(t *testing.T) {
	h := newHarness(t, nil)
	p := newPeer(t)
	old := p.visible(t, h.clock.now(), nil, "")
	if err := h.mod.acceptStatus(ipOf(peerIP), old); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}
	// note: another node at the same IP takes the peer's entry.
	if err := h.mod.acceptStatus(ipOf(peerIP), newPeer(t).visible(t, h.clock.now(), nil, "")); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}
	fillNonces(&h.mod.nonces, h.clock.now().Add(statusTTL))

	if err := h.mod.acceptStatus(ipOf("192.168.1.21"), p.visible(t, h.clock.now(), nil, "")); err != nil {
		t.Fatalf("acceptStatus = %v; want nil", err)
	}
	if err := h.mod.acceptStatus(ipOf("192.168.1.22"), old); !errors.Is(err, errReplay) {
		t.Fatalf("replay of the node's earlier status: acceptStatus = %v; want %v", err, errReplay)
	}
}

// remember refuses a new nonce while the memory holds maxNonces.
func TestNonceMemoryRefusesWhenFull(t *testing.T) {
	var m nonceMemory
	now := time.Date(2026, 11, 5, 17, 0, 0, 0, time.UTC)
	fillNonces(&m, now.Add(statusTTL))

	proof := &nearby.StatusProof{Nonce: astral.NewNonce(), Timestamp: astral.Time(now)}
	if err := m.remember(astral.GenerateIdentity(), proof, now); !errors.Is(err, errNonceMemoryFull) {
		t.Fatalf("remember = %v; want %v", err, errNonceMemoryFull)
	}
}

// A nonce is remembered until statusTTL after its status's timestamp, whether the timestamp
// lies before or after the arrival.
func TestNonceLifetimeFollowsTheTimestamp(t *testing.T) {
	arrival := time.Date(2026, 11, 5, 17, 0, 0, 0, time.UTC)
	for _, offset := range []time.Duration{-4 * time.Minute, 0, 4 * time.Minute} {
		var m nonceMemory
		node := astral.GenerateIdentity()
		proof := &nearby.StatusProof{Nonce: astral.NewNonce(), Timestamp: astral.Time(arrival.Add(offset))}
		if err := m.remember(node, proof, arrival); err != nil {
			t.Fatalf("remember: %v", err)
		}

		end := arrival.Add(offset + statusTTL)
		if !m.seen(node, proof.Nonce, end) {
			t.Errorf("timestamp %v from the arrival: nonce forgotten at %v; want it seen", offset, end)
		}
		if m.seen(node, proof.Nonce, end.Add(time.Nanosecond)) {
			t.Errorf("timestamp %v from the arrival: nonce seen after %v; want it forgotten", offset, end)
		}
	}
}

// The life of a nonce is wall time, like the freshness check: it carries no monotonic
// clock reading, also when the arrival time does.
func TestNonceLifetimeIsWallTime(t *testing.T) {
	var m nonceMemory
	node := astral.GenerateIdentity()
	arrival := time.Now()
	proof := &nearby.StatusProof{Nonce: astral.NewNonce(), Timestamp: astral.Time(arrival.Add(-time.Minute))}
	if err := m.remember(node, proof, arrival); err != nil {
		t.Fatalf("remember: %v", err)
	}

	until := m.nodes[node.String()][proof.Nonce]
	if until != until.Round(0) {
		t.Fatalf("nonce lives until %v, a monotonic clock reading; want wall time", until)
	}
}
