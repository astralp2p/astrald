package nearby

import (
	"bytes"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
)

// PeerSeen is emitted when a node is new to the cache, when its IP changes, and when it
// returns after its entry expired; not on a refresh from the same IP.
func TestPeerSeenEmission(t *testing.T) {
	h := newHarness(t, nil)
	p := newPeer(t)
	const otherIP = "192.168.1.21"

	steps := []struct {
		name    string
		advance time.Duration
		expire  bool // note: run the expiry ticker's removal before the status
		src     string
		events  int // note: PeerSeen emitted so far
	}{
		{"first seen", 0, false, peerIP, 1},
		{"refresh from the same IP", time.Minute, false, peerIP, 1},
		{"new IP", time.Minute, false, otherIP, 2},
		{"refresh from the new IP", time.Minute, false, otherIP, 2},
		{"back after expiry, entry not yet removed", statusTTL + time.Second, false, otherIP, 3},
		{"back after expiry, entry removed", statusTTL + time.Second, true, otherIP, 4},
	}

	for _, step := range steps {
		now := h.clock.advance(step.advance)
		if step.expire {
			h.mod.cache.removeExpired(now)
		}
		if err := h.mod.acceptStatus(ipOf(step.src), p.visible(t, now, nil, "")); err != nil {
			t.Fatalf("%s: acceptStatus: %v", step.name, err)
		}

		seen := h.events.peersSeen(t)
		if len(seen) != step.events {
			t.Fatalf("%s: emitted %d PeerSeen; want %d", step.name, len(seen), step.events)
		}
		if last := seen[len(seen)-1]; !bytes.Equal(last.SourceIP, ipOf(step.src)) {
			t.Fatalf("%s: last PeerSeen names %v; want %v", step.name, last.SourceIP, step.src)
		}
		if e, ok := h.cached(step.src); !ok || !e.Seen.Equal(now) || h.cacheLen() != 1 {
			t.Fatalf("%s: cache holds %d entries, entry at %v %+v; want one, refreshed at %v", step.name, h.cacheLen(), step.src, e, now)
		}
	}
}

// A Silent node with silent_listen off caches a verified status without an event; every
// other combination emits PeerSeen.
func TestPeerSeenInSilentFollowsSilentListen(t *testing.T) {
	no, yes := false, true
	cases := []struct {
		name         string
		mode         nearby.Mode
		silentListen *bool
		events       int
	}{
		{"silent, silent_listen unset", nearby.ModeSilent, nil, 1},
		{"silent, silent_listen on", nearby.ModeSilent, &yes, 1},
		{"silent, silent_listen off", nearby.ModeSilent, &no, 0},
		{"visible, silent_listen off", nearby.ModeVisible, &no, 1},
		{"stealth, silent_listen off", nearby.ModeStealth, &no, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, astral.GenerateIdentity())
			h.setMode(t, tc.mode)
			if tc.silentListen != nil {
				h.setSilentListen(t, *tc.silentListen)
			}

			if err := h.mod.acceptStatus(ipOf(peerIP), newPeer(t).visible(t, h.clock.now(), nil, "")); err != nil {
				t.Fatalf("acceptStatus: %v", err)
			}
			if _, ok := h.cached(peerIP); !ok {
				t.Fatal("status not cached")
			}
			if seen := h.events.peersSeen(t); len(seen) != tc.events {
				t.Fatalf("emitted %d PeerSeen; want %d", len(seen), tc.events)
			}
		})
	}
}

// capIP returns the i-th source IP of the cap test.
func capIP(i int) ip.IP {
	return ip.IP(net.IPv4(10, 1, byte(i>>8), byte(i)).To4())
}

// The cache holds at most maxCacheEntries: a status from a new IP is dropped while it is
// full, before identification, and a known IP is still refreshed.
func TestCacheCap(t *testing.T) {
	if maxCacheEntries != 512 {
		t.Fatalf("cache cap %d; want the spec's 512", maxCacheEntries)
	}
	h := newHarness(t, nil)
	rec := userRecognizer(astral.GenerateIdentity())
	h.mod.recognizers.Add(rec)
	unrecognised := newPeer(t).stealth(t, h.clock.now(), astral.GenerateIdentity())

	for i := range maxCacheEntries {
		if err := h.mod.acceptStatus(capIP(i), unrecognised); err != nil {
			t.Fatalf("status %d: acceptStatus: %v", i, err)
		}
	}
	calls := rec.calls.Load()

	err := h.mod.acceptStatus(capIP(maxCacheEntries), newPeer(t).visible(t, h.clock.now(), nil, ""))
	if !errors.Is(err, errCacheFull) {
		t.Fatalf("new IP in a full cache: acceptStatus = %v; want %v", err, errCacheFull)
	}
	if n := h.cacheLen(); n != maxCacheEntries {
		t.Fatalf("cache holds %d entries; want %d", n, maxCacheEntries)
	}
	if rec.calls.Load() != calls || h.crypto.verifies.Load() != 0 {
		t.Fatalf("a dropped status reached identification or verification")
	}

	now := h.clock.advance(time.Minute)
	p := newPeer(t)
	if err := h.mod.acceptStatus(capIP(7), p.visible(t, now, nil, "")); err != nil {
		t.Fatalf("known IP in a full cache: acceptStatus = %v; want nil", err)
	}
	if e, ok := h.cached(capIP(7).String()); !ok || !e.NodeID.IsEqual(p.node) || !e.Seen.Equal(now) {
		t.Fatalf("known IP: cached %+v; want the refreshed status of %v", e, p.node)
	}
	if seen := h.events.peersSeen(t); len(seen) != 1 {
		t.Fatalf("emitted %d PeerSeen; want 1", len(seen))
	}
}

// The expiry ticker removes, every 30 s, the cache entries and nonces that expired, and
// keeps the live ones.
func TestExpiryTicker(t *testing.T) {
	h := newHarness(t, nil)
	old, fresh := newPeer(t), newPeer(t)

	if err := h.mod.acceptStatus(ipOf(peerIP), old.visible(t, h.clock.now(), nil, "")); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}
	h.clock.advance(3 * time.Minute)
	if err := h.mod.acceptStatus(ipOf("192.168.1.21"), fresh.visible(t, h.clock.now(), nil, "")); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.mod.runExpiry(ctx)
	}()

	w := h.clock.nextWait(t)
	if w.d != expiryInterval {
		t.Fatalf("ticker waits %v; want %v", w.d, expiryInterval)
	}
	w.fire(h.clock.advance(2*time.Minute + time.Second))
	h.clock.nextWait(t) // note: the next tick starts after the removal

	if _, ok := h.cached(peerIP); ok || h.cacheLen() != 1 {
		t.Fatalf("cache holds %d entries, expired one present %v; want only the live one", h.cacheLen(), ok)
	}
	if n := h.mod.nonces.size(); n != 1 {
		t.Fatalf("nonce memory holds %d nonces; want the live status's 1", n)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("the ticker did not stop with its context")
	}
}

// An unrecognised stealth status from the IP of a live verified peer is dropped: the peer
// keeps its endpoints and its alias, and its next status emits no PeerSeen. Once the
// verified entry expired, an unrecognised status takes the IP.
func TestUnrecognisedStatusLeavesAVerifiedPeer(t *testing.T) {
	h := newHarness(t, nil)
	p := newPeer(t)
	if err := h.mod.acceptStatus(ipOf(peerIP), p.visible(t, h.clock.now(), nil, "phone")); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}

	forged := newPeer(t).stealth(t, h.clock.now(), astral.GenerateIdentity())
	if err := h.mod.acceptStatus(ipOf(peerIP), forged); !errors.Is(err, errVerifiedHeld) {
		t.Fatalf("unrecognised status at a verified IP: acceptStatus = %v; want %v", err, errVerifiedHeld)
	}
	if n := len(resolveEndpoints(t, h.mod, p.node)); n != 2 {
		t.Fatalf("resolved %d endpoints of the verified peer; want 2", n)
	}
	if id, err := h.mod.ResolveIdentity(".phone"); err != nil || !id.IsEqual(p.node) {
		t.Fatalf("ResolveIdentity(.phone) = %v, %v; want %v", id, err, p.node)
	}

	now := h.clock.advance(20 * time.Second)
	if err := h.mod.acceptStatus(ipOf(peerIP), p.visible(t, now, nil, "phone")); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}
	if seen := h.events.peersSeen(t); len(seen) != 1 {
		t.Fatalf("emitted %d PeerSeen; want the first sight only", len(seen))
	}

	now = h.clock.advance(statusTTL + time.Second)
	if err := h.mod.acceptStatus(ipOf(peerIP), newPeer(t).stealth(t, now, astral.GenerateIdentity())); err != nil {
		t.Fatalf("unrecognised status after the verified entry expired: acceptStatus = %v; want nil", err)
	}
	if e, ok := h.cached(peerIP); !ok || e.NodeID != nil {
		t.Fatalf("cached %+v, %v; want the unrecognised entry", e, ok)
	}
}
