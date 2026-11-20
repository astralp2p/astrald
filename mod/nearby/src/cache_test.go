package nearby

import (
	"bytes"
	"errors"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
)

// A node claimed while cached emits PeerSeen with its new user, although it keeps its IP
// and its entry is live; a refresh with the same user emits nothing.
func TestPeerSeenOnANewUser(t *testing.T) {
	user := astral.GenerateIdentity()
	h := newHarness(t, user)
	h.mod.recognizers.Add(userRecognizer(user))
	p := newPeer(t)

	steps := []struct {
		name   string
		status func(now time.Time) *nearby.StatusMessage
		user   *astral.Identity // note: the UserID of the last PeerSeen
		events int              // note: PeerSeen emitted so far
	}{
		{"unowned", func(now time.Time) *nearby.StatusMessage { return p.visible(t, now, nil, "") }, nil, 1},
		{"claimed, Stealth", func(now time.Time) *nearby.StatusMessage { return p.stealth(t, now, user) }, user, 2},
		{"claimed, refresh", func(now time.Time) *nearby.StatusMessage { return p.stealth(t, now, user) }, user, 2},
		{"claimed, Visible", func(now time.Time) *nearby.StatusMessage { return p.visible(t, now, user, "") }, user, 2},
	}

	for _, step := range steps {
		now := h.clock.advance(time.Minute)
		if err := h.mod.acceptStatus(ipOf(peerIP), step.status(now)); err != nil {
			t.Fatalf("%s: acceptStatus: %v", step.name, err)
		}

		seen := h.events.peersSeen(t)
		if len(seen) != step.events {
			t.Fatalf("%s: emitted %d PeerSeen; want %d", step.name, len(seen), step.events)
		}
		if last := seen[len(seen)-1]; !last.NodeID.IsEqual(p.node) || !last.UserID.IsEqual(step.user) {
			t.Fatalf("%s: last PeerSeen names node %v, user %v; want %v, %v", step.name, last.NodeID, last.UserID, p.node, step.user)
		}
	}
}

// portsStatus returns the Visible status of p that carries ports, or none when ports is nil.
func portsStatus(t *testing.T, p *peer, now time.Time, user *astral.Identity, ports *nearby.ListenPorts) *nearby.StatusMessage {
	t.Helper()
	profile := &nearby.PublicProfile{NodeID: p.node, UserID: user}
	if ports == nil {
		return p.status(t, now, false, profile)
	}
	return p.status(t, now, false, profile, ports)
}

// A verified status from a cached node at the same IP and with the same user emits exactly
// one PeerSeen when its effective listen ports differ from the cached ones, and none when
// they are equal. A status without ports has the effective ports 1791 and 1792.
func TestPeerSeenOnAPortChange(t *testing.T) {
	user := astral.GenerateIdentity()
	h := newHarness(t, user)
	p := newPeer(t)

	steps := []struct {
		name   string
		ports  *nearby.ListenPorts
		events int // note: PeerSeen emitted so far
	}{
		{"first seen, no ports", nil, 1},
		{"refresh, no ports", nil, 1},
		{"refresh, the default ports stated", &nearby.ListenPorts{TCP: 1791, KCP: 1792}, 1},
		{"refresh, no ports again", nil, 1},
		{"new ports", &nearby.ListenPorts{TCP: 1800, KCP: 1801}, 2},
		{"refresh, the same ports", &nearby.ListenPorts{TCP: 1800, KCP: 1801}, 2},
		{"new tcp port only", &nearby.ListenPorts{TCP: 1900, KCP: 1801}, 3},
		{"new kcp port only", &nearby.ListenPorts{TCP: 1900, KCP: 1901}, 4},
		{"kcp no longer listening", &nearby.ListenPorts{TCP: 1900}, 5},
		{"back to the defaults, no ports", nil, 6},
		{"refresh, no ports", nil, 6},
	}

	for _, step := range steps {
		now := h.clock.advance(time.Minute)
		if err := h.mod.acceptStatus(ipOf(peerIP), portsStatus(t, p, now, user, step.ports)); err != nil {
			t.Fatalf("%s: acceptStatus: %v", step.name, err)
		}

		seen := h.events.peersSeen(t)
		if len(seen) != step.events {
			t.Fatalf("%s: emitted %d PeerSeen; want %d", step.name, len(seen), step.events)
		}
		if last := seen[len(seen)-1]; !last.NodeID.IsEqual(p.node) || !last.UserID.IsEqual(user) || !bytes.Equal(last.SourceIP, ipOf(peerIP)) {
			t.Fatalf("%s: last PeerSeen names node %v, user %v, IP %v; want %v, %v, %v", step.name, last.NodeID, last.UserID, last.SourceIP, p.node, user, peerIP)
		}
		if e, ok := h.cached(peerIP); !ok || !e.Seen.Equal(now) || h.cacheLen() != 1 {
			t.Fatalf("%s: cache holds %d entries, entry at %v %+v; want one, refreshed at %v", step.name, h.cacheLen(), peerIP, e, now)
		}
	}
}

// A Stealth node that restarts on a new port sends new sealed ports under the same user, and
// that is one PeerSeen; the same sealed ports under a new hint nonce are none.
func TestPeerSeenOnASealedPortChange(t *testing.T) {
	user := astral.GenerateIdentity()
	h := newHarness(t, user)
	h.mod.recognizers.Add(userRecognizer(user))
	p := newPeer(t)

	steps := []struct {
		name   string
		ports  nearby.ListenPorts
		events int
	}{
		{"first seen", nearby.ListenPorts{TCP: 1800, KCP: 1801}, 1},
		{"refresh, the same ports, a new nonce", nearby.ListenPorts{TCP: 1800, KCP: 1801}, 1},
		{"new ports", nearby.ListenPorts{TCP: 1900, KCP: 1801}, 2},
	}

	for _, step := range steps {
		now := h.clock.advance(time.Minute)
		hint := stealthHint(t, p.node, user)
		status := p.status(t, now, true, hint, sealPorts(t, p.node, hint, step.ports))
		if err := h.mod.acceptStatus(ipOf(peerIP), status); err != nil {
			t.Fatalf("%s: acceptStatus: %v", step.name, err)
		}
		if seen := h.events.peersSeen(t); len(seen) != step.events {
			t.Fatalf("%s: emitted %d PeerSeen; want %d", step.name, len(seen), step.events)
		}
	}
}

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

// Two nodes on one IP that announce different tcp ports hold one entry each; each resolves
// to its own ports, and their refreshes emit no PeerSeen: the nodes no longer evict each
// other.
func TestTwoNodesOneIPBothCached(t *testing.T) {
	h := newHarness(t, nil)
	x, y := newPeer(t), newPeer(t)
	xPorts := &nearby.ListenPorts{TCP: 1800, KCP: 1801}
	yPorts := &nearby.ListenPorts{TCP: 2800, KCP: 2801}

	for round := range 3 {
		now := h.clock.advance(time.Minute)
		if err := h.mod.acceptStatus(ipOf(peerIP), portsStatus(t, x, now, nil, xPorts)); err != nil {
			t.Fatalf("round %d: node X: acceptStatus: %v", round, err)
		}
		if err := h.mod.acceptStatus(ipOf(peerIP), portsStatus(t, y, now, nil, yPorts)); err != nil {
			t.Fatalf("round %d: node Y: acceptStatus: %v", round, err)
		}

		if n := h.cacheLen(); n != 2 {
			t.Fatalf("round %d: cache holds %d entries; want one per node", round, n)
		}
		if e, ok := h.cachedAt(peerIP, 1800); !ok || !e.isNode(x.node) || !e.Seen.Equal(now) {
			t.Fatalf("round %d: entry at %v:1800 = %+v, %v; want the refreshed status of node X", round, peerIP, e, ok)
		}
		if e, ok := h.cachedAt(peerIP, 2800); !ok || !e.isNode(y.node) || !e.Seen.Equal(now) {
			t.Fatalf("round %d: entry at %v:2800 = %+v, %v; want the refreshed status of node Y", round, peerIP, e, ok)
		}
		if seen := h.events.peersSeen(t); len(seen) != 2 {
			t.Fatalf("round %d: emitted %d PeerSeen; want 2, one first sight per node", round, len(seen))
		}
	}

	if got := endpointStrings(t, resolveEndpoints(t, h.mod, x.node)); !slices.Equal(got, []string{"tcp:1800", "kcp:1801"}) {
		t.Fatalf("node X resolved %v; want its own ports", got)
	}
	if got := endpointStrings(t, resolveEndpoints(t, h.mod, y.node)); !slices.Equal(got, []string{"tcp:2800", "kcp:2801"}) {
		t.Fatalf("node Y resolved %v; want its own ports", got)
	}
}

// A verified node that re-broadcasts from the same IP with a new tcp port leaves exactly
// one entry, under the new port, and another node at that IP keeps its entry. A new kcp
// port alone keeps the key.
func TestPortMoveLeavesOneEntry(t *testing.T) {
	h := newHarness(t, nil)
	mover, bystander := newPeer(t), newPeer(t)

	accept := func(p *peer, ports *nearby.ListenPorts) {
		t.Helper()
		if err := h.mod.acceptStatus(ipOf(peerIP), portsStatus(t, p, h.clock.advance(time.Minute), nil, ports)); err != nil {
			t.Fatalf("acceptStatus: %v", err)
		}
	}
	accept(bystander, &nearby.ListenPorts{TCP: 2800, KCP: 2801})
	accept(mover, &nearby.ListenPorts{TCP: 1800, KCP: 1801})
	accept(mover, &nearby.ListenPorts{TCP: 1900, KCP: 1801})

	if n := h.cacheLen(); n != 2 {
		t.Fatalf("cache holds %d entries; want the mover's one and the bystander's one", n)
	}
	if _, ok := h.cachedAt(peerIP, 1800); ok {
		t.Fatalf("entry under the old port %v:1800 remains", peerIP)
	}
	if e, ok := h.cachedAt(peerIP, 1900); !ok || !e.isNode(mover.node) {
		t.Fatalf("entry at %v:1900 = %+v, %v; want the mover", peerIP, e, ok)
	}
	if e, ok := h.cachedAt(peerIP, 2800); !ok || !e.isNode(bystander.node) {
		t.Fatalf("entry at %v:2800 = %+v, %v; want the bystander", peerIP, e, ok)
	}
	if got := endpointStrings(t, resolveEndpoints(t, h.mod, mover.node)); !slices.Equal(got, []string{"tcp:1900", "kcp:1801"}) {
		t.Fatalf("mover resolved %v; want only its new ports", got)
	}
	if seen := h.events.peersSeen(t); len(seen) != 3 {
		t.Fatalf("emitted %d PeerSeen; want 3: two first sights and the port move", len(seen))
	}

	accept(mover, &nearby.ListenPorts{TCP: 1900, KCP: 1950})
	if n := h.cacheLen(); n != 2 {
		t.Fatalf("after a kcp port change the cache holds %d entries; want 2", n)
	}
	if e, ok := h.cachedAt(peerIP, 1900); !ok || e.Ports.KCP != 1950 {
		t.Fatalf("entry at %v:1900 = %+v, %v; want the status with kcp 1950", peerIP, e, ok)
	}
	if seen := h.events.peersSeen(t); len(seen) != 4 {
		t.Fatalf("emitted %d PeerSeen; want 4: the kcp port change is one", len(seen))
	}
}

// An unrecognised stealth status keys on the default tcp port. It is rejected with
// errVerifiedHeld while a live verified entry holds that IP on the default port, and
// accepted beside a verified entry that uses another port, which keeps its entry.
func TestUnrecognisedStatusKeysOnTheDefaultPort(t *testing.T) {
	forge := func(h *harness) *nearby.StatusMessage {
		return newPeer(t).stealth(t, h.clock.now(), astral.GenerateIdentity())
	}

	t.Run("verified on the default port", func(t *testing.T) {
		h := newHarness(t, nil)
		p := newPeer(t)
		ports := &nearby.ListenPorts{TCP: nearby.DefaultTCPPort, KCP: 1950}
		if err := h.mod.acceptStatus(ipOf(peerIP), portsStatus(t, p, h.clock.now(), nil, ports)); err != nil {
			t.Fatalf("acceptStatus: %v", err)
		}

		if err := h.mod.acceptStatus(ipOf(peerIP), forge(h)); !errors.Is(err, errVerifiedHeld) {
			t.Fatalf("acceptStatus = %v; want %v", err, errVerifiedHeld)
		}
		if e, ok := h.cachedAt(peerIP, nearby.DefaultTCPPort); h.cacheLen() != 1 || !ok || !e.isNode(p.node) {
			t.Fatalf("cache holds %d entries, default-port entry %+v; want the verified node's only", h.cacheLen(), e)
		}
	})

	t.Run("verified on another port", func(t *testing.T) {
		h := newHarness(t, nil)
		p := newPeer(t)
		ports := &nearby.ListenPorts{TCP: 1800, KCP: 1801}
		if err := h.mod.acceptStatus(ipOf(peerIP), portsStatus(t, p, h.clock.now(), nil, ports)); err != nil {
			t.Fatalf("acceptStatus: %v", err)
		}

		if err := h.mod.acceptStatus(ipOf(peerIP), forge(h)); err != nil {
			t.Fatalf("acceptStatus = %v; want nil", err)
		}
		if n := h.cacheLen(); n != 2 {
			t.Fatalf("cache holds %d entries; want the verified node's and the unrecognised one", n)
		}
		if e, ok := h.cachedAt(peerIP, nearby.DefaultTCPPort); !ok || e.NodeID != nil {
			t.Fatalf("default-port entry %+v, %v; want the unrecognised status", e, ok)
		}
		if e, ok := h.cachedAt(peerIP, 1800); !ok || !e.isNode(p.node) {
			t.Fatalf("entry at %v:1800 = %+v, %v; want the verified node", peerIP, e, ok)
		}
		if got := endpointStrings(t, resolveEndpoints(t, h.mod, p.node)); !slices.Equal(got, []string{"tcp:1800", "kcp:1801"}) {
			t.Fatalf("verified node resolved %v; want its ports", got)
		}
	})

	t.Run("verified node on the default port after the unrecognised status", func(t *testing.T) {
		h := newHarness(t, nil)
		if err := h.mod.acceptStatus(ipOf(peerIP), forge(h)); err != nil {
			t.Fatalf("acceptStatus = %v; want nil", err)
		}

		p := newPeer(t)
		if err := h.mod.acceptStatus(ipOf(peerIP), p.visible(t, h.clock.now(), nil, "")); err != nil {
			t.Fatalf("acceptStatus: %v", err)
		}
		if e, ok := h.cachedAt(peerIP, nearby.DefaultTCPPort); h.cacheLen() != 1 || !ok || !e.isNode(p.node) {
			t.Fatalf("cache holds %d entries, default-port entry %+v; want the verified node's only", h.cacheLen(), e)
		}
	})
}

// A full cache refuses a status under a new key, also at an IP that holds an entry, and
// accepts a status under an existing key and a verified node's move to a new port at its
// IP, which frees the key it leaves. A node's move to a new IP is refused before
// identification, while its old entry stays.
func TestCacheCapIsPerKey(t *testing.T) {
	h := newHarness(t, nil)
	unrecognised := newPeer(t).stealth(t, h.clock.now(), astral.GenerateIdentity())
	mover := newPeer(t)

	if err := h.mod.acceptStatus(ipOf(peerIP), mover.visible(t, h.clock.now(), nil, "")); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}
	for i := range maxCacheEntries - 1 {
		if err := h.mod.acceptStatus(capIP(i), unrecognised); err != nil {
			t.Fatalf("status %d: acceptStatus: %v", i, err)
		}
	}
	if n := h.cacheLen(); n != maxCacheEntries {
		t.Fatalf("cache holds %d entries; want %d", n, maxCacheEntries)
	}

	if h.mod.cache.hasRoom(capIP(maxCacheEntries).String()) {
		t.Fatalf("hasRoom for an IP without an entry in a full cache; want false")
	}
	if !h.mod.cache.hasRoom(capIP(7).String()) {
		t.Fatalf("hasRoom for an IP with an entry in a full cache; want true")
	}

	// A new key at an IP that holds an entry passes the pre-check and is refused at the store.
	other := newPeer(t)
	ports := &nearby.ListenPorts{TCP: 1800, KCP: 1801}
	err := h.mod.acceptStatus(capIP(7), portsStatus(t, other, h.clock.now(), nil, ports))
	if !errors.Is(err, errCacheFull) {
		t.Fatalf("new key at a known IP: acceptStatus = %v; want %v", err, errCacheFull)
	}
	if _, ok := h.cachedAt(capIP(7).String(), 1800); ok || h.cacheLen() != maxCacheEntries {
		t.Fatalf("cache holds %d entries, new key present %v; want %d and the new key refused", h.cacheLen(), ok, maxCacheEntries)
	}
	if len(resolveEndpoints(t, h.mod, other.node)) != 0 {
		t.Fatal("a refused status resolved endpoints")
	}

	// A replacement under an existing key is accepted.
	now := h.clock.advance(time.Minute)
	replacer := newPeer(t)
	if err := h.mod.acceptStatus(capIP(7), replacer.visible(t, now, nil, "")); err != nil {
		t.Fatalf("existing key: acceptStatus = %v; want nil", err)
	}
	if e, ok := h.cachedAt(capIP(7).String(), nearby.DefaultTCPPort); !ok || !e.isNode(replacer.node) || h.cacheLen() != maxCacheEntries {
		t.Fatalf("entry at %v = %+v, %v, %d entries; want the replacing status and %d entries", capIP(7), e, ok, h.cacheLen(), maxCacheEntries)
	}

	// A node's move to an IP that holds no entry is refused before identification, and its
	// old entry stays.
	now = h.clock.advance(time.Minute)
	err = h.mod.acceptStatus(capIP(maxCacheEntries), mover.visible(t, now, nil, ""))
	if !errors.Is(err, errCacheFull) {
		t.Fatalf("move to a new IP in a full cache: acceptStatus = %v; want %v", err, errCacheFull)
	}
	if e, ok := h.cachedAt(peerIP, nearby.DefaultTCPPort); !ok || !e.isNode(mover.node) || h.cacheLen() != maxCacheEntries {
		t.Fatalf("entry at %v:1791 = %+v, %v, %d entries; want the mover's old entry and %d entries", peerIP, e, ok, h.cacheLen(), maxCacheEntries)
	}

	// A node's move to a new port takes the key it leaves, so it fits in a full cache.
	now = h.clock.advance(time.Minute)
	if err := h.mod.acceptStatus(ipOf(peerIP), portsStatus(t, mover, now, nil, ports)); err != nil {
		t.Fatalf("port move in a full cache: acceptStatus = %v; want nil", err)
	}
	if _, ok := h.cachedAt(peerIP, nearby.DefaultTCPPort); ok {
		t.Fatalf("entry under the old port %v:1791 remains", peerIP)
	}
	if e, ok := h.cachedAt(peerIP, 1800); !ok || !e.isNode(mover.node) || h.cacheLen() != maxCacheEntries {
		t.Fatalf("entry at %v:1800 = %+v, %v, %d entries; want the mover and %d entries", peerIP, e, ok, h.cacheLen(), maxCacheEntries)
	}
}
