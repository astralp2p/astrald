package nearby

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
)

// Statuses of a Stealth swarm node seen by an unclaimed node, whose only recognizer is
// the user module's join recognizer. See mod/user/src/status_recognizer.go.

// A Stealth status heard before user.join runs is cached by IP only and resolves nothing.
// Once the swarm node is the join target, its next status from the same IP is verified,
// replaces the IP-only entry, emits PeerSeen and resolves tcp :1791 and kcp :1792.
func TestJoinRecognitionReplacesTheUnrecognisedEntry(t *testing.T) {
	h := newHarness(t, nil)
	rec, target := joinRecognizer()
	h.mod.recognizers.Add(rec)
	swarm, swarmUser := newPeer(t), astral.GenerateIdentity()

	if err := h.mod.acceptStatus(ipOf(peerIP), swarm.stealth(t, h.clock.now(), swarmUser)); err != nil {
		t.Fatalf("status before the join: acceptStatus = %v", err)
	}
	if e, ok := h.cached(peerIP); !ok || e.NodeID != nil {
		t.Fatalf("cached %+v, %v before the join; want an unrecognised entry", e, ok)
	}
	if n := len(resolveEndpoints(t, h.mod, swarm.node)); n != 0 {
		t.Fatalf("resolved %d endpoints before the join; want 0", n)
	}

	target.Store(swarm.node)
	if err := h.mod.acceptStatus(ipOf(peerIP), swarm.stealth(t, h.clock.now(), swarmUser)); err != nil {
		t.Fatalf("status during the join: acceptStatus = %v", err)
	}

	e, ok := h.cached(peerIP)
	if !ok || !e.NodeID.IsEqual(swarm.node) || !e.UserID.IsEqual(swarmUser) || e.Via != nearby.ViaHint {
		t.Fatalf("cached %+v, %v; want node %v of %v, Via %q", e, ok, swarm.node, swarmUser, nearby.ViaHint)
	}
	if n := h.cacheLen(); n != 1 {
		t.Fatalf("cache holds %d entries; want the swarm node's 1", n)
	}
	seen := h.events.peersSeen(t)
	if len(seen) != 1 || !seen[0].NodeID.IsEqual(swarm.node) || !seen[0].UserID.IsEqual(swarmUser) {
		t.Fatalf("PeerSeen = %+v; want one naming %v of %v", seen, swarm.node, swarmUser)
	}
	requireEndpointsAt(t, h, swarm.node, peerIP)
}

// A replay of the recognised status from another IP fails the nonce check and leaves the
// swarm node at its IP.
func TestJoinRecognitionRefusesAReplayFromAnotherIP(t *testing.T) {
	h := newHarness(t, nil)
	rec, target := joinRecognizer()
	h.mod.recognizers.Add(rec)
	swarm, swarmUser := newPeer(t), astral.GenerateIdentity()
	target.Store(swarm.node)

	status := swarm.stealth(t, h.clock.now(), swarmUser)
	if err := h.mod.acceptStatus(ipOf(peerIP), status); err != nil {
		t.Fatalf("first acceptStatus: %v", err)
	}
	const attackerIP = "192.168.1.66"
	if err := h.mod.acceptStatus(ipOf(attackerIP), status); !errors.Is(err, errReplay) {
		t.Fatalf("replay from %v: acceptStatus = %v; want %v", attackerIP, err, errReplay)
	}

	if _, ok := h.cached(attackerIP); ok {
		t.Fatalf("the replay is cached at %v", attackerIP)
	}
	if e, ok := h.cached(peerIP); !ok || !e.NodeID.IsEqual(swarm.node) {
		t.Fatalf("cached %+v, %v at %v; want the swarm node", e, ok, peerIP)
	}
	requireEndpointsAt(t, h, swarm.node, peerIP)
}

// A status whose hint opens with the join target but whose proof is not signed by the
// target's key fails verification and caches nothing.
func TestJoinRecognitionRefusesAForgedHint(t *testing.T) {
	h := newHarness(t, nil)
	rec, target := joinRecognizer()
	h.mod.recognizers.Add(rec)
	swarm, forger := newPeer(t), newPeer(t)
	target.Store(swarm.node)

	forged := forger.status(t, h.clock.now(), true, stealthHint(t, swarm.node, astral.GenerateIdentity()))
	if err := h.mod.acceptStatus(ipOf(peerIP), forged); !errors.Is(err, errBadProof) {
		t.Fatalf("forged status: acceptStatus = %v; want %v", err, errBadProof)
	}
	h.requireNothingStored(t)
	if n := len(resolveEndpoints(t, h.mod, swarm.node)); n != 0 {
		t.Fatalf("resolved %d endpoints of the forged node; want 0", n)
	}
}

// requireEndpointsAt fails unless node resolves exactly tcp src:1791 and kcp src:1792.
func requireEndpointsAt(t *testing.T, h *harness, node *astral.Identity, src string) {
	t.Helper()
	var got []string
	for _, e := range resolveEndpoints(t, h.mod, node) {
		got = append(got, e.Endpoint.Network()+":"+e.Endpoint.Address())
	}
	want := []string{"tcp:" + src + ":1791", "kcp:" + src + ":1792"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("resolved %v; want %v", got, want)
	}
}
