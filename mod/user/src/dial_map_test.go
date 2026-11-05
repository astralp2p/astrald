package user

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// testEpoch is the fake clock's start.
var testEpoch = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// TestDialMapAllowsOneAttemptPerInterval: a node gets one attempt per
// dialInterval, and another node is unaffected.
func TestDialMapAllowsOneAttemptPerInterval(t *testing.T) {
	m := newDialMap()
	node, other := astral.GenerateIdentity(), astral.GenerateIdentity()

	steps := []struct {
		node *astral.Identity
		at   time.Duration
		want bool
	}{
		{node, 0, true},
		{node, 0, false},
		{other, 0, true},
		{node, dialInterval - time.Nanosecond, false},
		{node, dialInterval, true},
		{node, dialInterval + time.Second, false},
	}
	for i, s := range steps {
		if got := m.reserve(s.node, testEpoch.Add(s.at)); got != s.want {
			t.Fatalf("step %v: reserve at +%v = %v; want %v", i, s.at, got, s.want)
		}
	}
}

// TestDialMapDelay: a delayed node gets no attempt until its delay ends.
func TestDialMapDelay(t *testing.T) {
	m := newDialMap()
	node := astral.GenerateIdentity()

	m.reserve(node, testEpoch)
	m.delay(node, testEpoch.Add(dialIntervalAfterRefusal))

	if m.reserve(node, testEpoch.Add(dialIntervalAfterRefusal-time.Nanosecond)) {
		t.Fatal("an attempt inside the delay was allowed")
	}
	if !m.reserve(node, testEpoch.Add(dialIntervalAfterRefusal)) {
		t.Fatal("the attempt at the end of the delay was refused")
	}
}

// TestDialMapCap: a full map refuses a new node and still serves the nodes it
// holds; expired entries make room again.
func TestDialMapCap(t *testing.T) {
	m := newDialMap()
	held := astral.GenerateIdentity()
	m.reserve(held, testEpoch)
	m.delay(held, testEpoch.Add(dialIntervalAfterRefusal))
	for len(m.next) < dialMapCap {
		m.reserve(astral.GenerateIdentity(), testEpoch)
	}

	if m.reserve(astral.GenerateIdentity(), testEpoch.Add(time.Second)) {
		t.Fatal("a full map took a new node")
	}
	m.delay(astral.GenerateIdentity(), testEpoch.Add(dialIntervalAfterRefusal))
	if len(m.next) != dialMapCap {
		t.Fatalf("the map holds %v nodes; want the cap %v", len(m.next), dialMapCap)
	}

	if !m.reserve(astral.GenerateIdentity(), testEpoch.Add(dialInterval)) {
		t.Fatal("a new node was refused after the other entries expired")
	}
	if len(m.next) != 2 {
		t.Fatalf("the map holds %v nodes after the prune; want the delayed node and the new one", len(m.next))
	}
	if m.reserve(held, testEpoch.Add(dialInterval)) {
		t.Fatal("the prune dropped a node whose delay has not ended")
	}
}
