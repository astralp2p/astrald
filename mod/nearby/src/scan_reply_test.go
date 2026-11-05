package nearby

import (
	"bytes"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ether"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
)

// scanFrom returns the broadcast event of a scan from src.
func scanFrom(src string) *ether.EventBroadcastReceived {
	return &ether.EventBroadcastReceived{SourceIP: ipOf(src), Object: &nearby.ScanMessage{}}
}

// newReplyHarness returns an unowned Visible node that answers scans.
func newReplyHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, nil)
	h.mod.AddStatusComposer(&modeComposer{mod: h.mod})
	return h
}

// requireSubnetReplies checks the node answered only by subnet broadcasts, n of them so
// far, each a status towards a scan source in srcs.
func requireSubnetReplies(t *testing.T, h *harness, n int, srcs ...string) {
	t.Helper()
	pushed, subnetted := h.ether.recorded()
	if len(pushed) != 0 {
		t.Fatalf("sent %d broadcasts to every subnet; want only subnet replies", len(pushed))
	}
	if len(subnetted) != n {
		t.Fatalf("sent %d subnet replies; want %d", len(subnetted), n)
	}
	for _, s := range subnetted {
		if _, ok := s.obj.(*nearby.StatusMessage); !ok {
			t.Fatalf("replied with %v; want a status", s.obj.ObjectType())
		}
		found := false
		for _, src := range srcs {
			found = found || bytes.Equal(s.src, ipOf(src))
		}
		if !found {
			t.Fatalf("replied towards %v; want one of %v", s.src, srcs)
		}
	}
}

// A scan is answered at once; a second scan inside the 2 s window schedules one reply at
// the window's end; a third adds nothing. Another subnet has its own window.
func TestScanReplyWindow(t *testing.T) {
	h := newReplyHarness(t)

	h.mod.receiveBroadcast(scanFrom(peerIP))
	h.ether.waitSend(t)
	requireSubnetReplies(t, h, 1, peerIP)

	h.clock.advance(500 * time.Millisecond)
	h.mod.receiveBroadcast(scanFrom("192.168.1.21"))
	deferred := h.clock.nextWait(t)
	if deferred.d != 1500*time.Millisecond {
		t.Fatalf("deferred reply waits %v; want the 1.5 s left in the window", deferred.d)
	}

	h.clock.advance(500 * time.Millisecond)
	h.mod.receiveBroadcast(scanFrom("192.168.1.22"))
	h.clock.requireNoWait(t)
	h.ether.requireNoSend(t)

	h.mod.receiveBroadcast(scanFrom("10.0.0.5"))
	h.ether.waitSend(t)
	requireSubnetReplies(t, h, 2, peerIP, "10.0.0.5")

	deferred.fire(h.clock.advance(time.Second))
	h.ether.waitSend(t)
	requireSubnetReplies(t, h, 3, peerIP, "10.0.0.5", "192.168.1.21")

	h.clock.advance(500 * time.Millisecond)
	h.mod.receiveBroadcast(scanFrom(peerIP))
	if w := h.clock.nextWait(t); w.d != 1500*time.Millisecond {
		t.Fatalf("a scan 0.5 s after the deferred reply waits %v; want 1.5 s", w.d)
	}
	h.ether.requireNoSend(t)
}

// A scan after the window is over is answered at once.
func TestScanReplyAfterTheWindow(t *testing.T) {
	h := newReplyHarness(t)

	h.mod.receiveBroadcast(scanFrom(peerIP))
	h.ether.waitSend(t)
	h.clock.advance(replyWindow)
	h.mod.receiveBroadcast(scanFrom(peerIP))
	h.ether.waitSend(t)

	h.clock.requireNoWait(t)
	requireSubnetReplies(t, h, 2, peerIP)
}

// No reply goes to a scan from outside every local subnet, nor from a Stealth node
// without an active contract.
func TestScanReplyNeedsALocalSubnetAndAStatus(t *testing.T) {
	h := newReplyHarness(t)
	h.mod.receiveBroadcast(scanFrom("203.0.113.7"))
	h.ether.requireNoSend(t)
	h.clock.requireNoWait(t)

	stealth := newHarness(t, astral.GenerateIdentity())
	stealth.mod.AddStatusComposer(&modeComposer{mod: stealth.mod}) // note: no hint without a contract
	stealth.mod.receiveBroadcast(scanFrom(peerIP))
	stealth.ether.requireNoSend(t)
}

// The reply state holds at most maxReplySubnets subnets, and a subnet whose window is
// over leaves it.
func TestScanRepliesBound(t *testing.T) {
	var r scanReplies
	now := time.Date(2026, 11, 5, 17, 0, 0, 0, time.UTC)

	for i := range maxReplySubnets {
		if a, _ := r.plan(capIP(i).String(), now); a != replyNow {
			t.Fatalf("subnet %d: plan = %v; want replyNow", i, a)
		}
	}
	if a, _ := r.plan("one too many", now); a != replyNone {
		t.Fatalf("plan past the bound = %v; want replyNone", a)
	}

	if a, _ := r.plan("one too many", now.Add(replyWindow)); a != replyNow {
		t.Fatalf("plan after every window ended = %v; want replyNow", a)
	}
	if n := len(r.subnets); n != 1 {
		t.Fatalf("reply state holds %d subnets; want 1", n)
	}
}
