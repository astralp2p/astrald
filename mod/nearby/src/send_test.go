package nearby

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
	secp256k1mod "github.com/astralp2p/astrald/mod/secp256k1"
)

// statusCase is a node state and the attachments its status holds before the proof.
type statusCase struct {
	name    string
	owned   bool
	stored  *nearby.Mode
	types   []string // note: nil when nothing is sent
	stealth bool
}

func statusCases() []statusCase {
	visible, stealth := nearby.ModeVisible, nearby.ModeStealth
	flag, profile, hint := nearby.NewFlag("").ObjectType(), (&nearby.PublicProfile{}).ObjectType(), (&nearby.StealthHint{}).ObjectType()
	return []statusCase{
		{"unowned", false, nil, []string{flag, profile}, false},
		{"owned, visible", true, &visible, []string{profile}, false},
		{"owned, mode unset", true, nil, []string{hint}, true},
		{"owned, stealth", true, &stealth, []string{hint}, true},
	}
}

// A status holds the composers' attachments and then one proof: the node key's signature
// over them, plain in Visible, wrapped in Stealth.
func TestStatusCarriesTheProofLast(t *testing.T) {
	for _, tc := range statusCases() {
		t.Run(tc.name, func(t *testing.T) {
			owner := astral.GenerateIdentity()
			h := newHarness(t, nil)
			if tc.owned {
				h.mod.User = testUser{id: owner}
			} else {
				owner = nil
			}
			if tc.stored != nil {
				h.setMode(t, *tc.stored)
			}
			h.mod.AddStatusComposer(&modeComposer{mod: h.mod, owner: owner, alias: "living-room"})

			s, err := h.mod.status()
			if err != nil || s == nil {
				t.Fatalf("status() = %v, %v; want a status", s, err)
			}
			requireStatusTypes(t, s, tc.types)
			requireProof(t, h, s, tc.stealth)

			if n := h.crypto.signers.Load(); n != 1 {
				t.Fatalf("NodeSigner called %d times for one status; want 1", n)
			}
		})
	}
}

func requireStatusTypes(t *testing.T, s *nearby.StatusMessage, want []string) {
	t.Helper()
	objs := s.Attachments.Objects()
	want = append(want, (&nearby.StatusProof{}).ObjectType())
	if len(objs) != len(want) {
		t.Fatalf("status holds %d attachments; want %v", len(objs), want)
	}
	for i, o := range objs {
		if o.ObjectType() != want[i] {
			t.Fatalf("attachment %d is %v; want %v", i, o.ObjectType(), want[i])
		}
	}
}

// requireProof checks the proof is the node's signature, plain or wrapped, stamped now.
func requireProof(t *testing.T, h *harness, s *nearby.StatusMessage, stealth bool) {
	t.Helper()
	others, proof, ok := nearby.SplitProof(s.Attachments.Objects())
	if !ok {
		t.Fatal("status has no valid proof")
	}
	if !proof.Timestamp.Time().Equal(h.clock.now()) {
		t.Fatalf("proof stamped %v; want %v", proof.Timestamp, h.clock.now())
	}
	hash, err := nearby.ProofHash(others, proof.Nonce, proof.Timestamp)
	if err != nil {
		t.Fatalf("ProofHash: %v", err)
	}

	key := secp256k1.FromIdentity(h.self)
	plain := secp256k1mod.VerifyASN1(key, hash, &crypto.Signature{Scheme: crypto.SchemeASN1, Data: astral.Bytes16(proof.Sig)})
	unwrapped := nearby.WrapProofSig(h.self, proof.Nonce, proof.Sig)
	wrapped := secp256k1mod.VerifyASN1(key, hash, &crypto.Signature{Scheme: crypto.SchemeASN1, Data: unwrapped})

	switch {
	case !stealth && plain != nil:
		t.Fatalf("visible proof does not verify: %v", plain)
	case stealth && plain == nil:
		t.Fatal("stealth proof verifies without unwrapping")
	case stealth && wrapped != nil:
		t.Fatalf("stealth proof does not verify after WrapProofSig: %v", wrapped)
	}
}

// A node sends nothing in Silent, and nothing in Stealth while its composers attach
// nothing (no active contract); it then signs nothing.
func TestStatusIsNilWithoutAttachmentsOrInSilent(t *testing.T) {
	silent := nearby.ModeSilent
	cases := []struct {
		name      string
		owner     *astral.Identity
		stored    *nearby.Mode
		composers uint64
	}{
		{"silent", astral.GenerateIdentity(), &silent, 0},
		{"stealth without a contract", astral.GenerateIdentity(), nil, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.owner)
			if tc.stored != nil {
				h.setMode(t, *tc.stored)
			}
			c := &modeComposer{mod: h.mod} // note: no owner, so no hint in Stealth
			h.mod.AddStatusComposer(c)

			if s, err := h.mod.status(); s != nil || err != nil {
				t.Fatalf("status() = %v, %v; want nil, nil", s, err)
			}
			if err := h.mod.Broadcast(); err != nil {
				t.Fatalf("Broadcast: %v", err)
			}
			h.ether.requireNoSend(t)
			if h.crypto.signers.Load() != 0 || c.calls.Load() != 2*tc.composers {
				t.Fatalf("signed %d times, composed %d times; want 0, %d", h.crypto.signers.Load(), c.calls.Load(), 2*tc.composers)
			}
		})
	}
}

// proofComposer tries to attach a status proof of its own.
type proofComposer struct{ err error }

func (c *proofComposer) ComposeStatus(comp nearbymod.Composition) {
	c.err = comp.Attach(&nearby.StatusProof{Nonce: 1, Sig: make([]byte, 8)})
}

// A composer cannot attach a StatusProof; the status keeps nearby's one proof, last.
func TestCompositionRefusesAStatusProof(t *testing.T) {
	h := newHarness(t, nil)
	h.mod.AddStatusComposer(&modeComposer{mod: h.mod})
	pc := &proofComposer{}
	h.mod.AddStatusComposer(pc)

	s, err := h.mod.status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !errors.Is(pc.err, nearbymod.ErrStatusProofAttached) {
		t.Fatalf("Attach(StatusProof) = %v; want %v", pc.err, nearbymod.ErrStatusProofAttached)
	}
	if _, _, ok := nearby.SplitProof(s.Attachments.Objects()); !ok {
		t.Fatal("status does not end in exactly one proof")
	}
}

// A status one node sends is verified and cached by another: plain in Visible, and in
// Stealth by a node of the same user.
func TestAStatusSentIsAcceptedByAPeer(t *testing.T) {
	for _, stealth := range []bool{false, true} {
		user := astral.GenerateIdentity()
		sender, receiver := newHarness(t, user), newHarness(t, user)
		if !stealth {
			sender.setMode(t, nearby.ModeVisible)
		}
		sender.mod.AddStatusComposer(&modeComposer{mod: sender.mod, owner: user})
		receiver.mod.recognizers.Add(userRecognizer(user))

		s, err := sender.mod.status()
		if err != nil || s == nil {
			t.Fatalf("status() = %v, %v", s, err)
		}
		if err := receiver.mod.acceptStatus(ipOf(peerIP), s); err != nil {
			t.Fatalf("stealth %v: acceptStatus: %v", stealth, err)
		}
		if e, ok := receiver.cached(peerIP); !ok || !e.NodeID.IsEqual(sender.self) || !e.UserID.IsEqual(user) {
			t.Fatalf("stealth %v: cached %+v; want the sender %v of user %v", stealth, e, sender.self, user)
		}
	}
}

// Broadcast sends the status on every local subnet; Scan sends a scan there.
func TestBroadcastAndScanPushToEverySubnet(t *testing.T) {
	h := newHarness(t, nil)
	h.mod.AddStatusComposer(&modeComposer{mod: h.mod})

	if err := h.mod.Broadcast(); err != nil {
		t.Fatalf("Broadcast: %v", err)
	}
	if err := h.mod.Scan(); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	pushed, subnetted := h.ether.recorded()
	if len(pushed) != 2 || len(subnetted) != 0 {
		t.Fatalf("sent %d broadcasts and %d subnet broadcasts; want 2 and 0", len(pushed), len(subnetted))
	}
	if _, ok := pushed[0].(*nearby.StatusMessage); !ok {
		t.Fatalf("first push is %v; want the status", pushed[0].ObjectType())
	}
	if _, ok := pushed[1].(*nearby.ScanMessage); !ok {
		t.Fatalf("second push is %v; want a scan", pushed[1].ObjectType())
	}
}

// A new local address sends the status and then a scan; an event that adds no address
// sends nothing.
func TestAddressChangeSendsTheStatusThenAScan(t *testing.T) {
	h := newHarness(t, nil)
	h.mod.AddStatusComposer(&modeComposer{mod: h.mod})

	h.mod.receiveNetworkAddressChanged(&ip.EventNetworkAddressChanged{Removed: []ip.IP{ipOf("192.168.1.30")}})
	h.ether.requireNoSend(t)

	h.mod.receiveNetworkAddressChanged(&ip.EventNetworkAddressChanged{Added: []ip.IP{ipOf("192.168.1.30")}})
	pushed, _ := h.ether.recorded()
	if len(pushed) != 2 {
		t.Fatalf("sent %d datagrams; want the status and a scan", len(pushed))
	}
	if _, ok := pushed[0].(*nearby.StatusMessage); !ok {
		t.Fatalf("first push is %v; want the status", pushed[0].ObjectType())
	}
	if _, ok := pushed[1].(*nearby.ScanMessage); !ok {
		t.Fatalf("second push is %v; want a scan", pushed[1].ObjectType())
	}
}

// The periodic loop sends at once, after each interval drawn from 85 to 95 s, and on a
// mode change.
func TestPeriodicStatusInterval(t *testing.T) {
	owner := astral.GenerateIdentity()
	h := newHarness(t, owner)
	h.setMode(t, nearby.ModeVisible)
	h.mod.AddStatusComposer(&modeComposer{mod: h.mod, owner: owner})

	ctx, cancel := astral.NewContext(nil).WithCancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.mod.runPeriodic(ctx)
	}()

	h.ether.waitSend(t)
	for range 3 {
		w := h.clock.nextWait(t)
		if w.d < minBroadcastInterval || w.d > maxBroadcastInterval {
			t.Fatalf("waits %v; want 85 s to 95 s", w.d)
		}
		w.fire(h.clock.advance(w.d))
		h.ether.waitSend(t)
	}

	h.clock.nextWait(t)
	h.setMode(t, nearby.ModeStealth)
	h.ether.waitSend(t)
	h.clock.nextWait(t)

	cancel()
	<-done

	pushed, _ := h.ether.recorded()
	if len(pushed) != 5 {
		t.Fatalf("sent %d statuses; want 5", len(pushed))
	}
	if _, ok := astral.First[*nearby.StealthHint](pushed[4].(*nearby.StatusMessage).Attachments.Objects()); !ok {
		t.Fatal("the status after the mode change holds no stealth hint")
	}
}

// broadcastInterval draws from the whole range [85 s, 95 s] and never outside it.
func TestBroadcastIntervalRange(t *testing.T) {
	lo, hi := 95*time.Second, 85*time.Second
	for range 10000 {
		d := broadcastInterval()
		if d < 85*time.Second || d > 95*time.Second {
			t.Fatalf("drew %v; want 85 s to 95 s", d)
		}
		lo, hi = min(lo, d), max(hi, d)
	}
	if lo > 86*time.Second || hi < 94*time.Second {
		t.Fatalf("10000 draws span %v to %v; want close to 85 s to 95 s", lo, hi)
	}
}

// A Silent node sends nothing on any path: the periodic status, an address change, a
// scan reply, its own scan, nearby.broadcast and nearby.scan.
func TestSilentSendsNothing(t *testing.T) {
	h := newHarness(t, astral.GenerateIdentity())
	h.setMode(t, nearby.ModeSilent)
	c := &modeComposer{mod: h.mod}
	h.mod.AddStatusComposer(c)
	h.mod.Auth = &recordingAuth{verdict: true}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()
	go h.mod.runPeriodic(ctx)
	h.clock.nextWait(t).fire(h.clock.advance(maxBroadcastInterval))
	h.clock.nextWait(t)

	h.mod.ReceiveObject(&recordingDrop{sender: h.self, object: &events.Event{
		Data: &ip.EventNetworkAddressChanged{Added: []ip.IP{ipOf("192.168.1.30")}},
	}})
	h.mod.receiveBroadcast(scanFrom(peerIP))
	if err := h.mod.Scan(); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, op := range []struct {
		name string
		fn   any
	}{{"nearby.broadcast", h.mod.OpBroadcast}, {"nearby.scan", h.mod.OpScan}} {
		w := newRecordingWriter()
		if err := route(t, op.fn, astral.GenerateIdentity(), op.name, w); err != nil {
			t.Fatalf("%v: %v", op.name, err)
		}
		if got := w.answers(t); len(got) != 1 || got[0].ObjectType() != (&astral.Ack{}).ObjectType() {
			t.Fatalf("%v answered %v; want ack", op.name, got)
		}
	}

	h.ether.requireNoSend(t)
	h.clock.requireNoWait(t)
	if c.calls.Load() != 0 || h.crypto.signers.Load() != 0 {
		t.Fatalf("composed %d and signed %d times in Silent; want 0", c.calls.Load(), h.crypto.signers.Load())
	}
}

// switchingComposer stores mode and then composes as the user module does, as when a mode
// change lands while a status is composed.
type switchingComposer struct {
	modeComposer
	t    *testing.T
	mode nearby.Mode
}

func (c *switchingComposer) ComposeStatus(comp nearbymod.Composition) {
	if err := c.mod.SetMode(astral.NewContext(nil), c.mode); err != nil {
		c.t.Errorf("SetMode: %v", err)
	}
	c.modeComposer.ComposeStatus(comp)
}

// A mode change while the composers run leaves the status whole: a stealth hint carries a
// wrapped signature and a public profile a plain one.
func TestStatusWrapFollowsTheAttachments(t *testing.T) {
	visible, stealth := nearby.ModeVisible, nearby.ModeStealth
	hint, profile := (&nearby.StealthHint{}).ObjectType(), (&nearby.PublicProfile{}).ObjectType()
	cases := []struct {
		name         string
		stored, next nearby.Mode
		types        []string
		wrapped      bool
	}{
		{"visible to stealth", visible, stealth, []string{hint}, true},
		{"stealth to visible", stealth, visible, []string{profile}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner := astral.GenerateIdentity()
			h := newHarness(t, owner)
			h.setMode(t, tc.stored)
			h.mod.AddStatusComposer(&switchingComposer{modeComposer: modeComposer{mod: h.mod, owner: owner}, t: t, mode: tc.next})

			s, err := h.mod.status()
			if err != nil || s == nil {
				t.Fatalf("status() = %v, %v; want a status", s, err)
			}
			requireStatusTypes(t, s, tc.types)
			requireProof(t, h, s, tc.wrapped)
		})
	}
}

// Every status carries a fresh nonce, so a peer accepts consecutive periodic statuses of
// one sender.
func TestEveryStatusCarriesAFreshNonce(t *testing.T) {
	sender, receiver := newHarness(t, nil), newHarness(t, nil)
	sender.mod.AddStatusComposer(&modeComposer{mod: sender.mod})

	seen := map[astral.Nonce]bool{}
	for i := range 3 {
		s, err := sender.mod.status()
		if err != nil || s == nil {
			t.Fatalf("status %d: status() = %v, %v", i, s, err)
		}
		_, proof, _ := nearby.SplitProof(s.Attachments.Objects())
		if seen[proof.Nonce] {
			t.Fatalf("status %d reuses the nonce %v", i, proof.Nonce)
		}
		seen[proof.Nonce] = true

		if err := receiver.mod.acceptStatus(ipOf(peerIP), s); err != nil {
			t.Fatalf("status %d: acceptStatus = %v; want nil", i, err)
		}
		sender.clock.advance(maxBroadcastInterval)
		receiver.clock.advance(maxBroadcastInterval)
	}
}
