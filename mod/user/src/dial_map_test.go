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

// TestDialMapCap: a full map takes a new node in place of the node whose next
// attempt comes first, and keeps serving the other nodes it holds; entries
// whose time has come are pruned.
func TestDialMapCap(t *testing.T) {
	m := newDialMap()
	held, first := astral.GenerateIdentity(), astral.GenerateIdentity()
	m.reserve(held, testEpoch)
	m.delay(held, testEpoch.Add(dialIntervalAfterRefusal))
	m.reserve(first, testEpoch)
	for len(m.next) < dialMapCap {
		m.reserve(astral.GenerateIdentity(), testEpoch.Add(time.Second))
	}

	newcomer := astral.GenerateIdentity()
	if !m.reserve(newcomer, testEpoch.Add(2*time.Second)) {
		t.Fatal("a full map refused a new node")
	}
	if _, ok := m.next[first.String()]; ok || len(m.next) != dialMapCap {
		t.Fatalf("the map holds %v nodes, the first-due node %v; want the cap without it", len(m.next), ok)
	}
	if m.reserve(held, testEpoch.Add(2*time.Second)) || m.reserve(newcomer, testEpoch.Add(3*time.Second)) {
		t.Fatal("a held node got an attempt before its time")
	}

	late := astral.GenerateIdentity()
	m.delay(late, testEpoch.Add(dialIntervalAfterRefusal))
	if _, ok := m.next[late.String()]; !ok || len(m.next) != dialMapCap {
		t.Fatalf("the map holds %v nodes, the delayed new node %v; want the cap with it", len(m.next), ok)
	}

	if !m.reserve(astral.GenerateIdentity(), testEpoch.Add(dialIntervalAfterRefusal)) || len(m.next) != 1 {
		t.Fatalf("the map holds %v nodes after every time came; want the new node alone", len(m.next))
	}
}
