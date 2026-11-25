package user

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
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

// Done: a join completes on an Approval.
func TestJoinCompletesOnApproval(t *testing.T) {
	f := newCarrierFixture(user.TierPrivilegedMember)
	req, reqID := f.request(t, user.TierMember)

	done := f.startJoin(t, astral.NewContext(nil), req, f.newCarrier())
	approvalID := answerJoin(t, f.swarm, f.keys, f.carrier, reqID, false)

	r := receiveJoin(t, done)
	if r.err != nil {
		t.Fatalf("join: %v", r.err)
	}
	m, ok := user.MembershipOf(r.sc)
	switch {
	case !ok || m.Tier != user.TierMember:
		t.Fatalf("membership tier: %v %v", ok, m)
	case !r.sc.Issuer.IsEqual(f.carrier) || !r.sc.Subject.IsEqual(f.joiner.self):
		t.Fatalf("membership parties: %v -> %v", r.sc.Issuer, r.sc.Subject)
	case !r.sc.ApprovalID.IsEqual(approvalID):
		t.Fatalf("membership links %v, want the Approval %v", r.sc.ApprovalID, approvalID)
	}
	verifySignedBy(t, f.carrier, r.sc.Contract, r.sc.IssuerSig)
	if got := f.swarm.CarriedOut(approvalID); len(got) != 1 {
		t.Fatalf("contracts carrying out the Approval in SwarmState: %d, want 1", len(got))
	}
	if !f.joiner.kept.IsNil() {
		t.Fatal("joiner still keeps its request after joining")
	}
}

// Done: a join resumes after a carrier restart within the hour.
func TestJoinResumesAfterCarrierRestart(t *testing.T) {
	f := newCarrierFixture(user.TierTrusted)
	req, reqID := f.request(t, user.TierMember)

	// the carrier stops (restart) while the request is pending
	ctx, cancel := astral.NewContext(nil).WithCancel()
	done := f.startJoin(t, ctx, req, f.newCarrier())
	cancel()
	if r := receiveJoin(t, done); r.err == nil {
		t.Fatal("join ended with a membership before any answer")
	}
	if f.joiner.kept != req {
		t.Fatal("joiner dropped its request when the carrier went away")
	}

	// a yes arrives while they are apart, within the hour
	f.clock.advance(50 * time.Minute)
	approvalID := answerJoin(t, f.swarm, f.keys, f.userID, reqID, false)

	again, againID := f.request(t, user.TierMember)
	if again != req || !againID.IsEqual(reqID) {
		t.Fatalf("joiner resent %v, want the same request %v", againID, reqID)
	}
	r := receiveJoin(t, f.startJoin(t, astral.NewContext(nil), again, f.newCarrier()))
	if r.err != nil {
		t.Fatalf("resumed join: %v", r.err)
	}
	if !r.sc.ApprovalID.IsEqual(approvalID) {
		t.Fatalf("membership links %v, want %v", r.sc.ApprovalID, approvalID)
	}

	// another restart after carrying out hands over the same membership
	sc, err := f.newCarrier().Carry(astral.NewContext(nil), req)
	if err != nil || sc != r.sc {
		t.Fatalf("carry after carrying out: %v, same contract %v", err, sc == r.sc)
	}
	if n := f.swarm.count(user.EntryRequests); n != 1 {
		t.Fatalf("requests in SwarmState: %d, want 1", n)
	}
	if n := f.swarm.count(user.EntryContracts); n != 1 {
		t.Fatalf("contracts in SwarmState: %d, want 1", n)
	}
}

// Done: a Decline ends the join, even beside an Approval.
func TestJoinEndsOnDecline(t *testing.T) {
	f := newCarrierFixture(user.TierPrivilegedMember)
	req, reqID := f.request(t, user.TierMember)

	done := f.startJoin(t, astral.NewContext(nil), req, f.newCarrier())
	answerJoin(t, f.swarm, f.keys, f.userID, reqID, true)
	answerJoin(t, f.swarm, f.keys, f.carrier, reqID, false)

	r := receiveJoin(t, done)
	if !errors.Is(r.err, errJoinDeclined) {
		t.Fatalf("join: %v, want %v", r.err, errJoinDeclined)
	}
	assertJoinEnded(t, f)
}

// Done: the timeout ends the join.
func TestJoinEndsOnTimeout(t *testing.T) {
	f := newCarrierFixture(user.TierPrivilegedMember)
	req, _ := f.request(t, user.TierMember)

	done := f.startJoin(t, astral.NewContext(nil), req, f.newCarrier())
	f.clock.advance(user.RequestTimeout + time.Second)

	r := receiveJoin(t, done)
	if !errors.Is(r.err, errJoinExpired) {
		t.Fatalf("join: %v, want %v", r.err, errJoinExpired)
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

		// why the timeout: a carrier that admits the request waits for an answer.
		ctx, cancel := astral.NewContext(nil).WithTimeout(time.Second)
		_, err := f.joiner.Join(ctx, req, f.newCarrier().Carry)
		cancel()
		if !errors.Is(err, errCarrierTier) {
			t.Fatalf("carrier at tier %q: %v, want %v", tier, err, errCarrierTier)
		}
		if n := f.swarm.count(user.EntryRequests); n != 0 {
			t.Fatalf("carrier at tier %q stored %d requests", tier, n)
		}
		if _, err := f.newCarrier().FileExpelRequest(astral.NewContext(nil), f.joiner.self); !errors.Is(err, errExpelFilerTier) {
			t.Fatalf("carrier at tier %q filed an expel request: %v", tier, err)
		}
	}
}

// Done: a Privileged join needs the user key's signature.
func TestJoinPrivilegedNeedsUserKey(t *testing.T) {
	f := newCarrierFixture(user.TierPrivilegedMember)
	req, reqID := f.request(t, user.TierPrivilegedMember)
	answerJoin(t, f.swarm, f.keys, f.carrier, reqID, false)

	// an Approval does not stand in for the user key
	if _, err := f.newCarrier().Carry(astral.NewContext(nil), req); !errors.Is(err, errUserKeyRequired) {
		t.Fatalf("carry without the user key: %v, want %v", err, errUserKeyRequired)
	}

	// a ceremony that cannot reach the user key signs nothing
	c := f.newCarrier()
	c.ceremony = newUserKeyCeremony(astral.GenerateIdentity(), f.keys, f.swarm, f.swarm)
	if _, err := c.Carry(astral.NewContext(nil), req); err == nil {
		t.Fatal("ceremony signed without the user key")
	}
	if n := f.swarm.count(user.EntryContracts); n != 0 {
		t.Fatalf("contracts in SwarmState: %d, want 0", n)
	}

	// the user key signs, and its text shows the tier
	c.ceremony = newUserKeyCeremony(f.userID, f.keys, f.swarm, f.swarm)
	sc, err := c.Carry(astral.NewContext(nil), req)
	if err != nil {
		t.Fatalf("carry with the user key: %v", err)
	}
	assertUserKeyMembership(t, sc, f.userID, req.Joiner)
	verifySignedBy(t, f.userID, sc.Contract, sc.IssuerSig)
	text := f.keys.texts[len(f.keys.texts)-1]
	if !strings.Contains(text, string(user.TierPrivilegedMember)) || len(text)+23 > 240 {
		t.Fatalf("ceremony text %q (%d bytes with its commitment)", text, len(text)+23)
	}
}

// assertUserKeyMembership fails t unless sc is a Privileged membership the
// user key issued to subject, with no link (provisional option c).
func assertUserKeyMembership(t *testing.T, sc *auth.SignedContract, userID, subject *astral.Identity) {
	t.Helper()
	m, ok := user.MembershipOf(sc)
	switch {
	case !ok || m.Tier != user.TierPrivilegedMember:
		t.Fatalf("membership tier: %v", ok)
	case !sc.Issuer.IsEqual(userID) || !sc.Subject.IsEqual(subject):
		t.Fatalf("membership parties: %v -> %v", sc.Issuer, sc.Subject)
	case sc.ApprovalID != nil:
		t.Fatalf("user-key membership links %v, want none", sc.ApprovalID)
	}
}

// The first-node bootstrap path: the user key signs the node's Privileged
// membership with no link, and the node signs as subject.
func TestBootstrapSignsPrivilegedMembership(t *testing.T) {
	f := newBootstrapFixture()
	key := newJoinKeyring()
	userID := key.newKey()

	sc, err := f.mod.signPrivilegedClaim(astral.NewContext(nil), key.key(userID))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	assertUserKeyMembership(t, sc, userID, f.nodeID)
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
	reqID, _ := astral.ResolveObjectID(req)
	done := f.startJoin(t, astral.NewContext(nil), req, f.newCarrier())
	answerJoin(t, f.swarm, f.keys, f.carrier, reqID, false)
	if r := receiveJoin(t, done); r.err != nil {
		t.Fatalf("join: %v", r.err)
	}

	other := newInvite(f.carrier, astral.GenerateIdentity(), user.TierMember, f.clock.now())
	if _, err = f.joiner.Accept(astral.NewContext(nil), other); err == nil {
		t.Fatal("joiner accepted another node's invite")
	}
}
