package user

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// carrierFixture is a swarm with a user key, a carrier at a tier and a joiner,
// all reading one clock.
type carrierFixture struct {
	clock   *joinClock
	swarm   *joinSwarm
	keys    *joinKeyring
	userID  *astral.Identity
	carrier *astral.Identity
	joiner  *swarmJoiner
}

func newCarrierFixture(carrierTier user.Tier) *carrierFixture {
	f := &carrierFixture{clock: newJoinClock(), keys: newJoinKeyring()}
	f.swarm = newJoinSwarm(f.clock)
	f.userID = f.keys.newKey()
	f.carrier = f.keys.newKey()
	if carrierTier != "" {
		f.swarm.setTier(f.userID, f.carrier, carrierTier)
	}
	f.joiner = newSwarmJoiner(f.keys.newKey(), f.keys, memJoinKeeper{}, nil)
	f.joiner.now = f.clock.now
	return f
}

// newWriter returns the carrier node's typed writes over the fixture's swarm.
func (f *carrierFixture) newWriter() *swarmWriter {
	return &swarmWriter{self: f.carrier, signer: f.keys, view: f.swarm, merge: f.swarm.merge}
}

// newCarrier returns a fresh carrier, as after a restart: it remembers nothing.
func (f *carrierFixture) newCarrier() *swarmCarrier {
	c := newSwarmCarrier(f.carrier, f.swarm, f.swarm, f.keys)
	c.poll = time.Millisecond
	c.now = f.clock.now
	return c
}

type joinResult struct {
	sc  *auth.SignedContract
	err error
}

// startJoin runs the joiner's Join through carrier on its own goroutine, and
// returns once the request is in SwarmState.
func (f *carrierFixture) startJoin(t *testing.T, ctx *astral.Context, req *user.SignedJoinRequest, c *swarmCarrier) <-chan joinResult {
	t.Helper()
	done := make(chan joinResult, 1)
	go func() {
		sc, err := f.joiner.Join(ctx, req, c.Carry)
		done <- joinResult{sc, err}
	}()
	awaitSwarmEntries(t, f.swarm, user.EntryRequests, 1)
	return done
}

func (f *carrierFixture) request(t *testing.T, tier user.Tier) (*user.SignedJoinRequest, *astral.ObjectID) {
	t.Helper()
	req, err := f.joiner.Request(astral.NewContext(nil), f.carrier, tier)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	id, _ := astral.ResolveObjectID(req)
	return req, id
}

func receiveJoin(t *testing.T, done <-chan joinResult) joinResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("join did not end")
	}
	return joinResult{}
}

// cosignAs returns the joiner's side of a carrier session for req.
func (f *carrierFixture) cosignAs(req *user.SignedJoinRequest) joinCosign {
	return func(ctx *astral.Context, half *auth.SignedContract) (*auth.SignedContract, error) {
		return f.joiner.cosign(ctx, req, half)
	}
}

// Done: a join signs the membership in the carrier session, with no answer
// in SwarmState: the carrier signs as issuer, the joiner as subject, and the
// carrier stores it with both signatures.
func TestJoinSignsTheMembershipInTheCarrierSession(t *testing.T) {
	f := newCarrierFixture(user.TierPrivilegedMember)
	req, reqID := f.request(t, user.TierMember)

	r := receiveJoin(t, f.startJoin(t, astral.NewContext(nil), req, f.newCarrier()))
	if r.err != nil {
		t.Fatalf("join: %v", r.err)
	}
	m, ok := user.MembershipOf(r.sc)
	switch {
	case !ok || m.Tier != user.TierMember:
		t.Fatalf("membership tier: %v %v", ok, m)
	case !r.sc.Issuer.IsEqual(f.carrier) || !r.sc.Subject.IsEqual(f.joiner.self):
		t.Fatalf("membership parties: %v -> %v", r.sc.Issuer, r.sc.Subject)
	case !r.sc.RequestID.IsEqual(reqID):
		t.Fatalf("membership names %v, want the request %v", r.sc.RequestID, reqID)
	}
	verifySignedBy(t, f.carrier, r.sc.Contract, r.sc.IssuerSig)
	verifySignedBy(t, f.joiner.self, r.sc.Contract, r.sc.SubjectSig)

	carrierMembership, _ := f.swarm.Membership(f.carrier)
	if want := user.JoinMembershipExpiry(req.JoinRequest, usermod.DefaultContractValidity, carrierMembership.ExpiresAt); !r.sc.ExpiresAt.Time().Equal(want.Time()) {
		t.Fatalf("membership expires %v, want %v: FiledAt plus validity, cut to the carrier's", r.sc.ExpiresAt, want)
	}
	if got := f.swarm.ContractsOf(reqID); len(got) != 1 || got[0].SubjectSig == nil {
		t.Fatalf("contracts answering the request in SwarmState: %v, want the membership with both signatures", got)
	}
	if !f.joiner.kept.IsNil() {
		t.Fatal("joiner still keeps its request after joining")
	}
}

// Done: a join resumes after the session drops: the joiner resends the same
// request and the carrier answers a membership with the same body; a resend
// after the membership was stored hands over the stored one.
func TestJoinResumesAfterADroppedSession(t *testing.T) {
	f := newCarrierFixture(user.TierTrusted)
	req, reqID := f.request(t, user.TierMember)

	// the session drops after the carrier answered
	var first *auth.SignedContract
	dropped := errors.New("connection dropped")
	_, err := f.joiner.Join(astral.NewContext(nil), req, func(ctx *astral.Context, req *user.SignedJoinRequest, _ joinCosign) (*auth.SignedContract, error) {
		return f.newCarrier().Carry(ctx, req, func(_ *astral.Context, half *auth.SignedContract) (*auth.SignedContract, error) {
			first = half
			return nil, dropped
		})
	})
	if !errors.Is(err, dropped) {
		t.Fatalf("join over a dropped session: %v, want %v", err, dropped)
	}
	if f.joiner.kept != req {
		t.Fatal("joiner dropped its request when the session dropped")
	}
	if n := f.swarm.count(user.EntryContracts); n != 0 {
		t.Fatalf("contracts in SwarmState after the drop: %d, want 0", n)
	}

	f.clock.advance(50 * time.Minute)
	again, againID := f.request(t, user.TierMember)
	if again != req || !againID.IsEqual(reqID) {
		t.Fatalf("joiner resent %v, want the same request %v", againID, reqID)
	}
	r := receiveJoin(t, f.startJoin(t, astral.NewContext(nil), again, f.newCarrier()))
	if r.err != nil {
		t.Fatalf("resumed join: %v", r.err)
	}
	if !sameBody(first.Contract, r.sc.Contract) {
		t.Fatal("the resumed join answered a membership with another body")
	}

	// another session after the membership was stored hands over the same one
	sc, err := f.newCarrier().Carry(astral.NewContext(nil), req, f.cosignAs(req))
	if err != nil || sc != r.sc {
		t.Fatalf("carry after storing: %v, same contract %v", err, sc == r.sc)
	}
	if n := f.swarm.count(user.EntryRequests); n != 1 {
		t.Fatalf("requests in SwarmState: %d, want 1", n)
	}
	if n := f.swarm.count(user.EntryContracts); n != 1 {
		t.Fatalf("contracts in SwarmState: %d, want 1", n)
	}
}

// Done: a Decline ends the join, and the carrier signs nothing for it.
func TestJoinEndsOnDecline(t *testing.T) {
	f := newCarrierFixture(user.TierPrivilegedMember)
	req, reqID := f.request(t, user.TierMember)
	answerJoin(t, f.swarm, f.keys, f.userID, reqID, true)

	_, err := f.joiner.Join(astral.NewContext(nil), req, f.newCarrier().Carry)
	if !errors.Is(err, errJoinDeclined) {
		t.Fatalf("join: %v, want %v", err, errJoinDeclined)
	}
	assertJoinEnded(t, f)
}

// Done: the timeout ends the join, at the joiner and at the carrier.
func TestJoinEndsOnTimeout(t *testing.T) {
	f := newCarrierFixture(user.TierPrivilegedMember)
	req, _ := f.request(t, user.TierMember)
	f.clock.advance(user.RequestTimeout + time.Second)

	if _, err := f.newCarrier().Carry(astral.NewContext(nil), req, f.cosignAs(req)); !errors.Is(err, errJoinExpired) {
		t.Fatalf("carry: %v, want %v", err, errJoinExpired)
	}
	if _, err := f.joiner.Join(astral.NewContext(nil), req, f.newCarrier().Carry); !errors.Is(err, errJoinExpired) {
		t.Fatalf("join: %v, want %v", err, errJoinExpired)
	}
	assertJoinEnded(t, f)

	// the expired request is not resent: a new join files a new request
	again, _ := f.request(t, user.TierMember)
	if again == req {
		t.Fatal("joiner resent an expired request")
	}
}

func assertJoinEnded(t *testing.T, f *carrierFixture) {
	t.Helper()
	if n := f.swarm.count(user.EntryContracts); n != 0 {
		t.Fatalf("contracts in SwarmState: %d, want 0", n)
	}
	if !f.joiner.kept.IsNil() {
		t.Fatal("joiner still keeps its request after the join ended")
	}
}

// Done: a Member carrier is refused.
func TestJoinMemberCarrierRefused(t *testing.T) {
	for _, tier := range []user.Tier{user.TierMember, ""} {
		f := newCarrierFixture(tier)
		req, _ := f.request(t, user.TierMember)

		_, err := f.joiner.Join(astral.NewContext(nil), req, f.newCarrier().Carry)
		if !errors.Is(err, errCarrierTier) {
			t.Fatalf("carrier at tier %q: %v, want %v", tier, err, errCarrierTier)
		}
		if n := f.swarm.count(user.EntryRequests); n != 0 {
			t.Fatalf("carrier at tier %q stored %d requests", tier, n)
		}
		if _, err := f.newWriter().FileExpelRequest(astral.NewContext(nil), f.joiner.self); !errors.Is(err, errExpelFilerTier) {
			t.Fatalf("carrier at tier %q filed an expel request: %v", tier, err)
		}
	}
}

// Done: a carrier issues only a tier its own tier may issue: Trusted issues
// Member, Privileged issues Member and Trusted. A request for a higher tier
// ends the join, and nothing is stored or signed for it.
func TestJoinCarrierIssuesOnlyTheTiersItsTierMay(t *testing.T) {
	for _, c := range []struct {
		carrier, asked user.Tier
		want           error
	}{
		{user.TierTrusted, user.TierMember, nil},
		{user.TierTrusted, user.TierTrusted, errTierAboveIssuer},
		{user.TierPrivilegedMember, user.TierTrusted, nil},
		{user.TierPrivilegedMember, user.TierMember, nil},
	} {
		f := newCarrierFixture(c.carrier)
		req, _ := f.request(t, c.asked)
		signatures := len(f.keys.texts)

		sc, err := f.joiner.Join(astral.NewContext(nil), req, f.newCarrier().Carry)
		if !errors.Is(err, c.want) {
			t.Fatalf("%s carrier, %s asked: %v, want %v", c.carrier, c.asked, err, c.want)
		}
		if c.want != nil {
			if n := f.swarm.count(user.EntryRequests) + f.swarm.count(user.EntryContracts); n != 0 || len(f.keys.texts) != signatures {
				t.Fatalf("%s carrier, %s asked: stored %d entries and signed %d", c.carrier, c.asked, n, len(f.keys.texts)-signatures)
			}
			assertJoinEnded(t, f)
			continue
		}
		if m, ok := user.MembershipOf(sc); !ok || m.Tier != c.asked {
			t.Fatalf("%s carrier, %s asked: membership %v", c.carrier, c.asked, sc)
		}
	}
}

// Done: the carrier stores no membership its joiner did not sign: a missing
// subject signature or another body ends the session with
// errNotSignedByJoiner.
func TestJoinNeedsTheJoinersSignature(t *testing.T) {
	for name, tamper := range map[string]func(sc *auth.SignedContract) *auth.SignedContract{
		"no subject signature": func(sc *auth.SignedContract) *auth.SignedContract {
			sc.SubjectSig = nil
			return sc
		},
		"another body": func(sc *auth.SignedContract) *auth.SignedContract {
			c := *sc.Contract
			c.ExpiresAt = astral.Time(c.ExpiresAt.Time().Add(time.Hour))
			return &auth.SignedContract{Contract: &c, IssuerSig: sc.IssuerSig, SubjectSig: sc.SubjectSig}
		},
	} {
		f := newCarrierFixture(user.TierPrivilegedMember)
		req, _ := f.request(t, user.TierMember)
		_, err := f.newCarrier().Carry(astral.NewContext(nil), req, func(ctx *astral.Context, half *auth.SignedContract) (*auth.SignedContract, error) {
			signed, err := f.joiner.cosign(ctx, req, half)
			if err != nil {
				return nil, err
			}
			return tamper(signed), nil
		})
		if !errors.Is(err, errNotSignedByJoiner) {
			t.Fatalf("%s: %v, want %v", name, err, errNotSignedByJoiner)
		}
		if n := f.swarm.count(user.EntryContracts); n != 0 {
			t.Fatalf("%s: contracts in SwarmState: %d, want 0", name, n)
		}
	}
}

// Done: the joiner co-signs only the membership its request asked for.
func TestJoinerSignsOnlyWhatItAskedFor(t *testing.T) {
	f := newCarrierFixture(user.TierPrivilegedMember)
	req, reqID := f.request(t, user.TierMember)
	other := astral.GenerateIdentity()
	otherID, _ := astral.ResolveObjectID(&astral.Ack{})
	for name, c := range map[string]struct {
		issuer, subject *astral.Identity
		tier            user.Tier
		req             *astral.ObjectID
	}{
		"another subject": {f.carrier, other, user.TierMember, reqID},
		"another tier":    {f.carrier, f.joiner.self, user.TierTrusted, reqID},
		"another request": {f.carrier, f.joiner.self, user.TierMember, otherID},
		"another issuer":  {other, f.joiner.self, user.TierMember, reqID},
	} {
		contract, err := user.NewMembership(c.issuer, c.subject, c.tier, c.req, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		half := &auth.SignedContract{Contract: contract, IssuerSig: testSig()}
		if _, err = f.joiner.cosign(astral.NewContext(nil), req, half); err == nil {
			t.Fatalf("%s: the joiner signed", name)
		}
	}
}

// Done: a request filed later than the carrier's time is refused.
func TestJoinRefusesARequestFiledLater(t *testing.T) {
	f := newCarrierFixture(user.TierPrivilegedMember)
	req, _ := f.request(t, user.TierMember)
	c := f.newCarrier()
	c.now = func() time.Time { return f.clock.now().Add(-time.Minute) }
	if _, err := c.Carry(astral.NewContext(nil), req, f.cosignAs(req)); !errors.Is(err, errFiledLater) {
		t.Fatalf("carry: %v, want %v", err, errFiledLater)
	}
}

// Done: a Privileged join needs the user key's signature.
func TestJoinPrivilegedNeedsUserKey(t *testing.T) {
	f := newCarrierFixture(user.TierPrivilegedMember)
	req, reqID := f.request(t, user.TierPrivilegedMember)
	answerJoin(t, f.swarm, f.keys, f.carrier, reqID, false)

	// an Approval does not stand in for the user key
	if _, err := f.newCarrier().Carry(astral.NewContext(nil), req, f.cosignAs(req)); !errors.Is(err, errUserKeyRequired) {
		t.Fatalf("carry without the user key: %v, want %v", err, errUserKeyRequired)
	}

	// a ceremony that cannot reach the user key signs nothing; a Coldcard's
	// press is the user action, so these ceremonies sign without
	// user.confirm_join
	c := f.newCarrier()
	c.ceremony = newUserKeyCeremony(astral.GenerateIdentity(), f.keys, f.swarm, f.swarm)
	c.ceremony.devicePress = true
	if _, err := c.Carry(astral.NewContext(nil), req, f.cosignAs(req)); err == nil {
		t.Fatal("ceremony signed without the user key")
	}
	if n := f.swarm.count(user.EntryContracts); n != 0 {
		t.Fatalf("contracts in SwarmState: %d, want 0", n)
	}

	// the user key signs, the joiner co-signs, and the text shows the tier
	c.ceremony = newUserKeyCeremony(f.userID, f.keys, f.swarm, f.swarm)
	c.ceremony.devicePress = true
	sc, err := c.Carry(astral.NewContext(nil), req, f.cosignAs(req))
	if err != nil {
		t.Fatalf("carry with the user key: %v", err)
	}
	assertUserKeyMembership(t, sc, f.userID, req.Joiner, reqID)
	verifySignedBy(t, f.userID, sc.Contract, sc.IssuerSig)
	verifySignedBy(t, req.Joiner, sc.Contract, sc.SubjectSig)
	text := f.keys.texts[len(f.keys.texts)-2]
	if !strings.Contains(text, string(user.TierPrivilegedMember)) || len(text)+23 > 240 {
		t.Fatalf("ceremony text %q (%d bytes with its commitment)", text, len(text)+23)
	}
}

// assertUserKeyMembership fails t unless sc is a Privileged membership the
// user key issued to subject, naming req as its RequestID (nil: none).
func assertUserKeyMembership(t *testing.T, sc *auth.SignedContract, userID, subject *astral.Identity, req *astral.ObjectID) {
	t.Helper()
	m, ok := user.MembershipOf(sc)
	switch {
	case !ok || m.Tier != user.TierPrivilegedMember:
		t.Fatalf("membership tier: %v", ok)
	case !sc.Issuer.IsEqual(userID) || !sc.Subject.IsEqual(subject):
		t.Fatalf("membership parties: %v -> %v", sc.Issuer, sc.Subject)
	case req == nil && sc.RequestID != nil:
		t.Fatalf("user-key membership names %v, want none", sc.RequestID)
	case req != nil && !req.IsEqual(sc.RequestID):
		t.Fatalf("user-key membership names %v, want %v", sc.RequestID, req)
	}
}

// The first-node bootstrap path: the user key signs the node's Privileged
// membership naming no request, and the node signs as subject.
func TestBootstrapSignsPrivilegedMembership(t *testing.T) {
	f := newBootstrapFixture()
	key := newJoinKeyring()
	userID := key.newKey()

	sc, err := f.mod.signPrivilegedClaim(astral.NewContext(nil), f.nodeID, key.key(userID))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	assertUserKeyMembership(t, sc, userID, f.nodeID, nil)
	if err = f.auth.VerifyContract(sc); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

// An invite is a JoinRequest the carrier pre-fills; the joiner signs it
// unchanged and joins through it.
func TestJoinThroughInvite(t *testing.T) {
	f := newCarrierFixture(user.TierTrusted)
	invite := newInvite(f.carrier, f.joiner.self, user.TierMember, f.clock.now())

	req, err := f.joiner.Accept(astral.NewContext(nil), invite)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if req.JoinRequest != invite {
		t.Fatal("joiner changed the invite")
	}
	if r := receiveJoin(t, f.startJoin(t, astral.NewContext(nil), req, f.newCarrier())); r.err != nil {
		t.Fatalf("join: %v", r.err)
	}

	other := newInvite(f.carrier, astral.GenerateIdentity(), user.TierMember, f.clock.now())
	if _, err = f.joiner.Accept(astral.NewContext(nil), other); err == nil {
		t.Fatal("joiner accepted another node's invite")
	}
}

// Done: the carrier session runs its four messages over one channel: the
// request, the membership its issuer signed, the membership the joiner signed,
// and the stored membership with the user identity.
func TestCarrierSessionOverTheWire(t *testing.T) {
	f := newCarrierFixture(user.TierTrusted)
	req, reqID := f.request(t, user.TierMember)
	joinerEnd, carrierEnd := net.Pipe()
	defer joinerEnd.Close()
	defer carrierEnd.Close()

	carried := make(chan error, 1)
	go func() {
		ch := channel.New(carrierEnd)
		var got *user.SignedJoinRequest
		if err := ch.Switch(channel.Expect(&got), channel.FailOnEOF()); err != nil {
			carried <- err
			return
		}
		carried <- carrySession(astral.NewContext(nil), ch, f.newCarrier(), got, f.userID)
	}()

	sc, root, err := joinSession(astral.NewContext(nil), channel.New(joinerEnd), req, f.cosignAs(req))
	if err != nil {
		t.Fatalf("join session: %v", err)
	}
	if err = <-carried; err != nil {
		t.Fatalf("carrier session: %v", err)
	}
	if !root.IsEqual(f.userID) {
		t.Fatalf("carrier confirmed the user %v, want %v", root, f.userID)
	}
	verifySignedBy(t, f.carrier, sc.Contract, sc.IssuerSig)
	verifySignedBy(t, f.joiner.self, sc.Contract, sc.SubjectSig)
	if got := f.swarm.ContractsOf(reqID); len(got) != 1 || got[0].SubjectSig == nil {
		t.Fatalf("stored %v; want the membership with both signatures", got)
	}
}

// Done: a carrier that ends the join answers its end, which the joiner reads
// as that end.
func TestCarrierSessionSendsTheEnd(t *testing.T) {
	f := newCarrierFixture(user.TierTrusted)
	req, _ := f.request(t, user.TierTrusted)
	joinerEnd, carrierEnd := net.Pipe()
	defer joinerEnd.Close()
	defer carrierEnd.Close()

	go func() {
		ch := channel.New(carrierEnd)
		var got *user.SignedJoinRequest
		if ch.Switch(channel.Expect(&got), channel.FailOnEOF()) == nil {
			_ = carrySession(astral.NewContext(nil), ch, f.newCarrier(), got, f.userID)
		}
	}()

	_, _, err := joinSession(astral.NewContext(nil), channel.New(joinerEnd), req, f.cosignAs(req))
	if !errors.Is(err, errTierAboveIssuer) {
		t.Fatalf("join session: %v, want %v", err, errTierAboveIssuer)
	}
}
