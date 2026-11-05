package nearby

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ether"
	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
)

const peerIP = "192.168.1.20"

// A Visible status of an unowned peer is verified, cached with its node and no user, and
// reported once.
func TestAcceptStatusCachesAVerifiedVisibleStatus(t *testing.T) {
	h := newHarness(t, nil)
	p := newPeer(t)
	now := h.clock.now()

	status := p.status(t, now, false,
		nearby.NewFlag("adoptable"),
		&nearby.PublicProfile{NodeID: p.node, NodeAlias: "living-room"},
	)
	if err := h.mod.acceptStatus(ipOf(peerIP), status); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}

	e, ok := h.cached(peerIP)
	switch {
	case !ok:
		t.Fatal("status not cached")
	case !e.NodeID.IsEqual(p.node) || e.UserID != nil || e.Via != nearby.ViaProfile || e.Status != status:
		t.Fatalf("cached {NodeID %v, UserID %v, Via %q}; want {%v, nil, %q} and the status as received",
			e.NodeID, e.UserID, e.Via, p.node, nearby.ViaProfile)
	case !e.Seen.Equal(now):
		t.Fatalf("Seen = %v; want %v", e.Seen, now)
	}

	seen := h.events.peersSeen(t)
	if len(seen) != 1 {
		t.Fatalf("emitted %d PeerSeen; want 1", len(seen))
	}
	want := nearby.PeerSeen{NodeID: p.node, Via: nearby.ViaProfile, SourceIP: ipOf(peerIP)}
	if got := seen[0]; !got.NodeID.IsEqual(want.NodeID) || got.UserID != nil || got.Via != want.Via || !bytes.Equal(got.SourceIP, want.SourceIP) {
		t.Fatalf("PeerSeen = %+v; want %+v", got, want)
	}
	if n := h.crypto.signers.Load(); n != 0 {
		t.Fatalf("NodeSigner called %d times on receive; want 0", n)
	}
}

// A status that went over the wire verifies: the proof hash of the decoded attachments
// equals the hash the sender signed.
func TestAcceptStatusVerifiesADecodedStatus(t *testing.T) {
	h := newHarness(t, nil)
	p := newPeer(t)
	user := astral.GenerateIdentity()

	data, err := astral.EncodeBytes(p.visible(t, h.clock.now(), user, "phone"))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := astral.DecodeAs[*nearby.StatusMessage](data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if err := h.mod.acceptStatus(ipOf(peerIP), decoded); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}
	if e, ok := h.cached(peerIP); !ok || !e.UserID.IsEqual(user) {
		t.Fatalf("cached %v, %v; want the status with UserID %v", e, ok, user)
	}
}

// dropCase is a status the receive order drops, the reason, and the recognizer the
// module asks, if any.
type dropCase struct {
	name       string
	status     func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage
	recognizer func(h *harness, p *peer) *fakeRecognizer
	want       error
	// early is true for a status dropped before identification: no recognizer is
	// asked and no signature is checked.
	early bool
}

func TestAcceptStatusDropsAMalformedStatus(t *testing.T) {
	cases := concatCases(shapeDropCases(), freshnessDropCases(), identifyDropCases(), proofDropCases())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			p := newPeer(t)
			var rec *fakeRecognizer
			if tc.recognizer != nil {
				rec = tc.recognizer(h, p)
				h.mod.recognizers.Add(rec)
			}

			err := h.mod.acceptStatus(ipOf(peerIP), tc.status(t, h, p))
			if !errors.Is(err, tc.want) {
				t.Fatalf("acceptStatus = %v; want %v", err, tc.want)
			}
			h.requireNothingStored(t)

			if tc.early && rec != nil && rec.calls.Load() != 0 {
				t.Errorf("asked the recognizer %d times; want 0 before identification", rec.calls.Load())
			}
			if tc.early && h.crypto.verifies.Load() != 0 {
				t.Errorf("checked %d signatures; want 0 before identification", h.crypto.verifies.Load())
			}
		})
	}
}

func concatCases(groups ...[]dropCase) (all []dropCase) {
	for _, g := range groups {
		all = append(all, g...)
	}
	return all
}

// shapeDropCases break receive step 1.
func shapeDropCases() []dropCase {
	profile := func(p *peer) *nearby.PublicProfile { return &nearby.PublicProfile{NodeID: p.node} }
	someone := func(*harness, *peer) *fakeRecognizer { return fixedRecognizer(astral.GenerateIdentity(), nil) }
	return []dropCase{
		{"nil status", func(*testing.T, *harness, *peer) *nearby.StatusMessage { return nil }, someone, errNoBundle, true},
		{"nil bundle", func(*testing.T, *harness, *peer) *nearby.StatusMessage { return &nearby.StatusMessage{} }, someone, errNoBundle, true},
		{"no identity attachment", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.status(t, h.clock.now(), false, nearby.NewFlag("adoptable"))
		}, someone, errIdentityCount, true},
		{"profile and hint", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.status(t, h.clock.now(), false, profile(p), stealthHint(t, p.node, astral.GenerateIdentity()))
		}, someone, errIdentityCount, true},
		{"two profiles", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.status(t, h.clock.now(), false, profile(p), &nearby.PublicProfile{NodeID: p.node, NodeAlias: "b"})
		}, someone, errIdentityCount, true},
		{"no proof", func(t *testing.T, _ *harness, p *peer) *nearby.StatusMessage {
			return statusOf(t, profile(p))
		}, someone, errProofPlacement, true},
		{"proof not last", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return statusOf(t, p.proof(t, h.clock.now(), false, profile(p)), profile(p))
		}, someone, errProofPlacement, true},
		{"two proofs", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			first := p.proof(t, h.clock.now(), false, profile(p))
			return statusOf(t, profile(p), first, p.proof(t, h.clock.now(), false, profile(p), first))
		}, someone, errProofPlacement, true},
		{"7-byte signature", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return withSig(t, p.visible(t, h.clock.now(), nil, ""), make([]byte, 7))
		}, someone, errSigLength, true},
		{"73-byte signature", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return withSig(t, p.visible(t, h.clock.now(), nil, ""), make([]byte, 73))
		}, someone, errSigLength, true},
	}
}

// freshnessDropCases break receive step 2.
func freshnessDropCases() []dropCase {
	at := func(d time.Duration) func(*testing.T, *harness, *peer) *nearby.StatusMessage {
		return func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.visible(t, h.clock.now().Add(d), nil, "")
		}
	}
	farAt := func(ts time.Time) func(*testing.T, *harness, *peer) *nearby.StatusMessage {
		return func(t *testing.T, _ *harness, p *peer) *nearby.StatusMessage { return p.visible(t, ts, nil, "") }
	}
	return []dropCase{
		{"stale timestamp", at(-statusTTL - time.Nanosecond), nil, errStale, true},
		{"future timestamp", at(statusTTL + time.Nanosecond), nil, errStale, true},
		{"timestamp at the end of time", farAt(time.Unix(0, 1<<63-1)), nil, errStale, true},
		{"timestamp before 1970", farAt(time.Unix(0, -1<<63)), nil, errStale, true},
	}
}

// identifyDropCases break receive step 4.
func identifyDropCases() []dropCase {
	visible := func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
		return p.visible(t, h.clock.now(), nil, "")
	}
	return []dropCase{
		{"zero node in the profile", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.status(t, h.clock.now(), false, &nearby.PublicProfile{NodeID: &astral.Identity{}})
		}, nil, errNoNodeID, false},
		{"no node in the profile", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.status(t, h.clock.now(), false, &nearby.PublicProfile{NodeAlias: "a"})
		}, nil, errNoNodeID, false},
		{"recognized node differs from the profile", visible, func(*harness, *peer) *fakeRecognizer {
			return fixedRecognizer(astral.GenerateIdentity(), nil)
		}, errNodeMismatch, false},
		{"recognizer names no node", visible, func(*harness, *peer) *fakeRecognizer {
			return fixedRecognizer(nil, nil)
		}, errNoNodeID, false},
		{"panicking recognizer", visible, func(*harness, *peer) *fakeRecognizer {
			return &fakeRecognizer{recognize: func(*nearby.StatusMessage) *nearbymod.Recognition {
				panic("recognizer panic")
			}}
		}, errPanic, false},
		{"status of the local node", func(t *testing.T, h *harness, _ *peer) *nearby.StatusMessage {
			self := &peer{key: h.crypto.key, node: h.self}
			return self.visible(t, h.clock.now(), nil, "")
		}, nil, errOwnStatus, false},
	}
}

// proofDropCases break receive steps 5 and 6.
func proofDropCases() []dropCase {
	user := astral.GenerateIdentity()
	owner := func(*harness, *peer) *fakeRecognizer { return userRecognizer(user) }
	return []dropCase{
		{"tampered attachment", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			signed := &nearby.PublicProfile{NodeID: p.node, NodeAlias: "phone"}
			sent := &nearby.PublicProfile{NodeID: p.node, NodeAlias: "phon3"}
			return statusOf(t, sent, p.proof(t, h.clock.now(), false, signed))
		}, nil, errBadProof, false},
		{"tampered timestamp", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			profile := &nearby.PublicProfile{NodeID: p.node}
			proof := p.proof(t, h.clock.now(), false, profile)
			proof.Timestamp = astral.Time(h.clock.now().Add(time.Second))
			return statusOf(t, profile, proof)
		}, nil, errBadProof, false},
		{"wrong key", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			profile := &nearby.PublicProfile{NodeID: p.node}
			return statusOf(t, profile, newPeer(t).proof(t, h.clock.now(), false, profile))
		}, nil, errBadProof, false},
		{"wrapped proof in a visible status", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.status(t, h.clock.now(), true, &nearby.PublicProfile{NodeID: p.node})
		}, nil, errBadProof, false},
		{"stealth proof not wrapped", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			return p.status(t, h.clock.now(), false, stealthHint(t, p.node, user))
		}, owner, errBadProof, false},
		{"stealth proof wrapped by another node", func(t *testing.T, h *harness, p *peer) *nearby.StatusMessage {
			hint := stealthHint(t, p.node, user)
			return statusOf(t, hint, newPeer(t).proof(t, h.clock.now(), true, hint))
		}, owner, errBadProof, false},
	}
}

// withSig returns status with its proof's signature replaced by sig.
func withSig(t *testing.T, status *nearby.StatusMessage, sig []byte) *nearby.StatusMessage {
	t.Helper()
	others, proof, ok := nearby.SplitProof(status.Attachments.Objects())
	if !ok {
		t.Fatal("status has no proof")
	}
	forged := *proof
	forged.Sig = sig
	return statusOf(t, append(others, &forged)...)
}

// A timestamp exactly statusTTL away on either side is fresh.
func TestAcceptStatusFreshnessBoundary(t *testing.T) {
	for _, d := range []time.Duration{-statusTTL, statusTTL} {
		h := newHarness(t, nil)
		if err := h.mod.acceptStatus(ipOf(peerIP), newPeer(t).visible(t, h.clock.now().Add(d), nil, "")); err != nil {
			t.Errorf("timestamp %v away: acceptStatus = %v; want nil", d, err)
		}
	}
}

// A replayed status is dropped from any IP, while its timestamp is fresh, and even after
// its first arrival is more than statusTTL ago.
func TestAcceptStatusDropsAReplayedNonce(t *testing.T) {
	h := newHarness(t, nil)
	p := newPeer(t)

	// note: stamped 4 minutes ahead of the receiver, so it stays fresh 9 minutes.
	status := p.visible(t, h.clock.now().Add(4*time.Minute), nil, "")
	if err := h.mod.acceptStatus(ipOf(peerIP), status); err != nil {
		t.Fatalf("first acceptStatus: %v", err)
	}

	verifies := h.crypto.verifies.Load()
	for _, src := range []string{peerIP, "192.168.1.21"} {
		if err := h.mod.acceptStatus(ipOf(src), status); !errors.Is(err, errReplay) {
			t.Fatalf("replay from %v: acceptStatus = %v; want %v", src, err, errReplay)
		}
	}
	if n := h.crypto.verifies.Load() - verifies; n != 0 {
		t.Fatalf("checked %d signatures of replays; want 0, the nonce check comes first", n)
	}

	h.clock.advance(6 * time.Minute)
	h.mod.nonces.removeExpired(h.clock.now())
	if err := h.mod.acceptStatus(ipOf("192.168.1.22"), status); !errors.Is(err, errReplay) {
		t.Fatalf("replay 6 minutes later: acceptStatus = %v; want %v", err, errReplay)
	}

	if err := h.mod.acceptStatus(ipOf(peerIP), p.visible(t, h.clock.now(), nil, "")); err != nil {
		t.Fatalf("a fresh nonce: acceptStatus = %v; want nil", err)
	}
	if n := h.cacheLen(); n != 1 {
		t.Fatalf("cache holds %d entries; want the peer's 1", n)
	}
}

// A Stealth status no recognizer recognizes is cached with its source IP only, without a
// signature check and without an event.
func TestAcceptStatusCachesAnUnrecognisedStealthStatus(t *testing.T) {
	for _, withRecognizer := range []bool{false, true} {
		h := newHarness(t, nil)
		if withRecognizer {
			h.mod.recognizers.Add(userRecognizer(astral.GenerateIdentity()))
		}
		p := newPeer(t)

		status := p.stealth(t, h.clock.now(), astral.GenerateIdentity())
		if err := h.mod.acceptStatus(ipOf(peerIP), status); err != nil {
			t.Fatalf("acceptStatus: %v", err)
		}

		e, ok := h.cached(peerIP)
		if !ok || e.NodeID != nil || e.UserID != nil || e.Via != nearby.ViaHint || e.Status != status {
			t.Fatalf("cached %+v, %v; want an entry with no identity, Via %q", e, ok, nearby.ViaHint)
		}
		if n := h.crypto.verifies.Load(); n != 0 {
			t.Errorf("checked %d signatures; want 0", n)
		}
		if n := h.mod.nonces.size(); n != 0 {
			t.Errorf("remembered %d nonces; want 0", n)
		}
		if seen := h.events.peersSeen(t); len(seen) != 0 {
			t.Errorf("emitted %d PeerSeen; want 0", len(seen))
		}
	}
}

// A Stealth status a recognizer opens is verified with the unwrapped signature and
// cached with the node and the user.
func TestAcceptStatusVerifiesARecognizedStealthStatus(t *testing.T) {
	user := astral.GenerateIdentity()
	h := newHarness(t, user)
	h.mod.recognizers.Add(userRecognizer(user))
	p := newPeer(t)

	if err := h.mod.acceptStatus(ipOf(peerIP), p.stealth(t, h.clock.now(), user)); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}

	e, ok := h.cached(peerIP)
	if !ok || !e.NodeID.IsEqual(p.node) || !e.UserID.IsEqual(user) || e.Via != nearby.ViaHint {
		t.Fatalf("cached %+v, %v; want node %v, user %v, Via %q", e, ok, p.node, user, nearby.ViaHint)
	}
	seen := h.events.peersSeen(t)
	if len(seen) != 1 || !seen[0].NodeID.IsEqual(p.node) || !seen[0].UserID.IsEqual(user) || seen[0].Via != nearby.ViaHint {
		t.Fatalf("PeerSeen = %+v; want one naming node %v, user %v, Via %q", seen, p.node, user, nearby.ViaHint)
	}
}

// The recognizers are asked in turn and the first that recognizes the status wins.
func TestAcceptStatusUsesTheFirstRecognizerThatRecognizes(t *testing.T) {
	h := newHarness(t, nil)
	p := newPeer(t)
	user := astral.GenerateIdentity()

	none := &fakeRecognizer{recognize: func(*nearby.StatusMessage) *nearbymod.Recognition { return nil }}
	first, second := fixedRecognizer(p.node, user), fixedRecognizer(p.node, astral.GenerateIdentity())
	h.mod.recognizers.Add(none, first, second)

	if err := h.mod.acceptStatus(ipOf(peerIP), p.visible(t, h.clock.now(), nil, "")); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}
	if e, _ := h.cached(peerIP); e == nil || !e.UserID.IsEqual(user) {
		t.Fatalf("cached %+v; want the first recognizer's user %v", e, user)
	}
	if none.calls.Load() != 1 || first.calls.Load() != 1 || second.calls.Load() != 0 {
		t.Fatalf("recognizer calls %d, %d, %d; want 1, 1, 0", none.calls.Load(), first.calls.Load(), second.calls.Load())
	}
}

// A zero UserID in a profile reads as no user.
func TestAcceptStatusReadsAZeroUserAsNone(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.mod.acceptStatus(ipOf(peerIP), newPeer(t).visible(t, h.clock.now(), &astral.Identity{}, "")); err != nil {
		t.Fatalf("acceptStatus: %v", err)
	}
	if e, _ := h.cached(peerIP); e == nil || e.UserID != nil {
		t.Fatalf("cached %+v; want UserID nil", e)
	}
	if seen := h.events.peersSeen(t); len(seen) != 1 || seen[0].UserID != nil {
		t.Fatalf("PeerSeen = %+v; want one with UserID nil", seen)
	}
}

// ReceiveObject acts only on what the local node delivers and never accepts a drop.
func TestReceiveObjectRequiresTheLocalSender(t *testing.T) {
	for _, local := range []bool{false, true} {
		h := newHarness(t, nil)
		h.mod.AddStatusComposer(&modeComposer{mod: h.mod})
		sender := astral.GenerateIdentity()
		if local {
			sender = h.self
		}

		status := &ether.EventBroadcastReceived{SourceIP: ipOf(peerIP), Object: newPeer(t).visible(t, h.clock.now(), nil, "")}
		changed := &events.Event{Data: &ip.EventNetworkAddressChanged{Added: []ip.IP{ipOf("192.168.1.30")}}}
		for _, obj := range []astral.Object{status, changed} {
			drop := &recordingDrop{sender: sender, object: obj}
			if err := h.mod.ReceiveObject(drop); err != nil {
				t.Fatalf("ReceiveObject: %v", err)
			}
			if a := drop.accepted(); len(a) != 0 {
				t.Fatalf("Accept calls %v; want none", a)
			}
		}

		if _, cached := h.cached(peerIP); cached != local {
			t.Fatalf("local sender %v: status cached %v; want %v", local, cached, local)
		}
		if pushed, _ := h.ether.recorded(); (len(pushed) == 2) != local {
			t.Fatalf("local sender %v: sent %d datagrams on the address change", local, len(pushed))
		}
	}
}
