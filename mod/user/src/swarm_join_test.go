package user

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// carrierFixture is a swarm with a user key, a carrier at a tier and a joiner,
// all reading one clock. The swarm is the carrier's module over a real store
// and view.
type carrierFixture struct {
	clock   *joinClock
	swarm   *Module
	keys    *joinKeyring
	userID  *astral.Identity
	carrier *astral.Identity
	joiner  *swarmJoiner
	// ceremony is where the user key signs on the carrier's node; nil: it reaches none.
	ceremony *userKeyCeremony
	// stored is signalled after each merge that stored entries.
	stored chan struct{}
	// base is what the swarm held before the test: the carrier's membership.
	base map[user.EntryKind]int
}

// count returns how many entries of kind the test stored.
func (f *carrierFixture) count(kind user.EntryKind) int {
	return countEntries(f.swarm, kind) - f.base[kind]
}

func newCarrierFixture(t testing.TB, carrierTier user.Tier) *carrierFixture {
	f := &carrierFixture{clock: newJoinClock(), keys: newJoinKeyring(), stored: make(chan struct{}, 1)}
	f.userID = f.keys.newKey()
	f.carrier = f.keys.newKey()
	f.swarm = newStoreModule(t, f.carrier, f.userID, f.keys, f.clock.now, acceptAll)
	f.swarm.writer = f.newWriter()
	f.swarm.swarmState.stored = func([]astral.Object) {
		select {
		case f.stored <- struct{}{}:
		default:
		}
	}
	if carrierTier != "" {
		storeMembership(t, f.swarm, f.userID, f.carrier, carrierTier, f.clock.now().Add(24*time.Hour))
	}
	f.base = map[user.EntryKind]int{user.EntryContracts: countEntries(f.swarm, user.EntryContracts)}
	f.joiner = newSwarmJoiner(astral.NewContext(nil), f.keys.newKey(), f.keys, &tree.Value[*user.SignedJoinRequest]{})
	f.joiner.now = f.clock.now
	return f
}

// newWriter returns the carrier node's typed writes over the fixture's swarm.
func (f *carrierFixture) newWriter() *swarmWriter {
	return &swarmWriter{self: f.carrier, signer: f.keys, view: f.swarm, merge: f.swarm.swarmState.merge}
}

// newCarrier returns a fresh carrier, as after a restart: it remembers nothing.
func (f *carrierFixture) newCarrier() *swarmCarrier {
	c := newSwarmCarrier(f.carrier, f.swarm, f.keys, &sig.Map[string, *auth.SignedContract]{})
	c.now = f.clock.now
	return c
}

// carry is the connection to c as the joiner's Join takes it: c.Carry with the
// fixture's ceremony.
func (f *carrierFixture) carry(c *swarmCarrier) func(*astral.Context, *user.SignedJoinRequest, joinCosign) (*auth.SignedContract, error) {
	return func(ctx *astral.Context, req *user.SignedJoinRequest, cosign joinCosign) (*auth.SignedContract, error) {
		return c.Carry(ctx, req, f.ceremony, cosign)
	}
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
		sc, err := f.joiner.Join(ctx, req, f.carry(c))
		done <- joinResult{sc, err}
	}()
	for f.count(user.EntryRequests) < 1 {
		select {
		case <-f.stored:
		case <-time.After(5 * time.Second):
			t.Fatal("the request was not stored")
		}
	}
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
	f := newCarrierFixture(t, user.TierPrivilegedMember)
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
	if !f.joiner.pending.Get().IsNil() {
		t.Fatal("joiner still keeps its request after joining")
	}
}

// Done: a join resumes after the session drops: the joiner resends the same
// request and the carrier answers a membership with the same body; a resend
// after the membership was stored hands over the stored one.
func TestJoinResumesAfterADroppedSession(t *testing.T) {
	f := newCarrierFixture(t, user.TierTrusted)
	req, reqID := f.request(t, user.TierMember)

	// the session drops after the carrier answered
	var first *auth.SignedContract
	dropped := errors.New("connection dropped")
	_, err := f.joiner.Join(astral.NewContext(nil), req, func(ctx *astral.Context, req *user.SignedJoinRequest, _ joinCosign) (*auth.SignedContract, error) {
		return f.newCarrier().Carry(ctx, req, nil, func(_ *astral.Context, half *auth.SignedContract) (*auth.SignedContract, error) {
			first = half
			return nil, dropped
		})
	})
	if !errors.Is(err, dropped) {
		t.Fatalf("join over a dropped session: %v, want %v", err, dropped)
	}
	if f.joiner.pending.Get() != req {
		t.Fatal("joiner dropped its request when the session dropped")
	}
	if n := f.count(user.EntryContracts); n != 0 {
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
	sc, err := f.newCarrier().Carry(astral.NewContext(nil), req, nil, f.cosignAs(req))
	if err != nil || sc != r.sc {
		t.Fatalf("carry after storing: %v, same contract %v", err, sc == r.sc)
	}
	if n := f.count(user.EntryRequests); n != 1 {
		t.Fatalf("requests in SwarmState: %d, want 1", n)
	}
	if n := f.count(user.EntryContracts); n != 1 {
		t.Fatalf("contracts in SwarmState: %d, want 1", n)
	}
}

// Done: two sessions that carry one JoinRequest at once store one
// membership: both have it co-signed before either stores it, and the second
// to store hands over the first one's.
func TestJoinConcurrentSessionsStoreOneMembership(t *testing.T) {
	f := newCarrierFixture(t, user.TierTrusted)
	req, reqID := f.request(t, user.TierMember)
	c := f.newCarrier()

	var signing sync.WaitGroup
	signing.Add(2)
	cosign := func(ctx *astral.Context, half *auth.SignedContract) (*auth.SignedContract, error) {
		signed, err := f.joiner.cosign(ctx, req, half)
		signing.Done()
		signing.Wait()
		return signed, err
	}
	results := make(chan joinResult, 2)
	for range 2 {
		go func() {
			sc, err := c.Carry(astral.NewContext(nil), req, nil, cosign)
			results <- joinResult{sc, err}
		}()
	}
	a, b := receiveJoin(t, results), receiveJoin(t, results)
	if a.err != nil || b.err != nil {
		t.Fatalf("carry: %v, %v", a.err, b.err)
	}
	idA, _ := astral.ResolveObjectID(a.sc)
	idB, _ := astral.ResolveObjectID(b.sc)
	if !idA.IsEqual(idB) {
		t.Fatal("the two sessions handed over two memberships")
	}
	if got := f.swarm.ContractsOf(reqID); len(got) != 1 {
		t.Fatalf("contracts answering the request in SwarmState: %d, want 1", len(got))
	}
}

// Done: an expelled joiner's join ends, before and after its membership was
// stored: the carrier signs nothing for it and hands over nothing.
func TestJoinEndsForAnExpelledJoiner(t *testing.T) {
	f := newCarrierFixture(t, user.TierTrusted)
	req, _ := f.request(t, user.TierMember)
	if r := receiveJoin(t, f.startJoin(t, astral.NewContext(nil), req, f.newCarrier())); r.err != nil {
		t.Fatalf("join: %v", r.err)
	}

	expulsion := &user.Expulsion{Issuer: f.userID, Subject: f.joiner.self, ExpelledAt: astral.Time(f.clock.now())}
	sig, err := f.keys.Sign(nil, secp256k1.FromIdentity(f.userID), expulsion)
	if err != nil {
		t.Fatalf("sign expulsion: %v", err)
	}
	if err = f.swarm.swarmState.merge(nil, &user.SignedExpulsion{Expulsion: expulsion, IssuerSig: sig}); err != nil {
		t.Fatalf("add expulsion: %v", err)
	}

	// the request it joined through, resent
	if _, err = f.newCarrier().Carry(astral.NewContext(nil), req, nil, f.cosignAs(req)); !errors.Is(err, errJoinDeclined) {
		t.Fatalf("carry of the fulfilled request: %v, want %v", err, errJoinDeclined)
	}
	// a new request
	again, _ := f.request(t, user.TierMember)
	if _, err = f.joiner.Join(astral.NewContext(nil), again, f.carry(f.newCarrier())); !errors.Is(err, errJoinDeclined) {
		t.Fatalf("join of a new request: %v, want %v", err, errJoinDeclined)
	}
	if n := f.count(user.EntryContracts); n != 1 {
		t.Fatalf("contracts in SwarmState: %d, want the one stored before the expel", n)
	}
}

// Done: a Decline ends the join, and the carrier signs nothing for it.
func TestJoinEndsOnDecline(t *testing.T) {
	f := newCarrierFixture(t, user.TierPrivilegedMember)
	req, reqID := f.request(t, user.TierMember)
	answerJoin(t, f.swarm, f.keys, f.userID, reqID, true)

	_, err := f.joiner.Join(astral.NewContext(nil), req, f.carry(f.newCarrier()))
	if !errors.Is(err, errJoinDeclined) {
		t.Fatalf("join: %v, want %v", err, errJoinDeclined)
	}
	assertJoinEnded(t, f)
}

// Done: the timeout ends the join, at the joiner and at the carrier.
func TestJoinEndsOnTimeout(t *testing.T) {
	f := newCarrierFixture(t, user.TierPrivilegedMember)
	req, _ := f.request(t, user.TierMember)
	f.clock.advance(user.RequestTimeout + time.Second)

	if _, err := f.newCarrier().Carry(astral.NewContext(nil), req, nil, f.cosignAs(req)); !errors.Is(err, errJoinExpired) {
		t.Fatalf("carry: %v, want %v", err, errJoinExpired)
	}
	if _, err := f.joiner.Join(astral.NewContext(nil), req, f.carry(f.newCarrier())); !errors.Is(err, errJoinExpired) {
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
	if n := f.count(user.EntryContracts); n != 0 {
		t.Fatalf("contracts in SwarmState: %d, want 0", n)
	}
	if !f.joiner.pending.Get().IsNil() {
		t.Fatal("joiner still keeps its request after the join ended")
	}
}

// Done: a Member carrier is refused.
func TestJoinMemberCarrierRefused(t *testing.T) {
	for _, tier := range []user.Tier{user.TierMember, ""} {
		t.Run(fmt.Sprintf("carrier at tier %q", tier), func(t *testing.T) {
			f := newCarrierFixture(t, tier)
			req, _ := f.request(t, user.TierMember)

			_, err := f.joiner.Join(astral.NewContext(nil), req, f.carry(f.newCarrier()))
			if !errors.Is(err, errCarrierTier) {
				t.Fatalf("join: %v, want %v", err, errCarrierTier)
			}
			if n := f.count(user.EntryRequests); n != 0 {
				t.Fatalf("carrier stored %d requests", n)
			}
			if _, err := f.newWriter().FileExpelRequest(astral.NewContext(nil), f.joiner.self); !errors.Is(err, errExpelFilerTier) {
				t.Fatalf("carrier filed an expel request: %v", err)
			}
		})
	}
}

// Done: a carrier issues only a tier its own tier may issue: Trusted issues
// Member, Privileged issues Member and Trusted. A request for a higher tier
// ends the join, and nothing is stored or signed for it.
func TestJoinCarrierIssuesOnlyTheTiersItsTierMay(t *testing.T) {
	for _, tc := range []struct {
		carrier, asked user.Tier
		want           error
	}{
		{user.TierTrusted, user.TierMember, nil},
		{user.TierTrusted, user.TierTrusted, errTierAboveIssuer},
		{user.TierPrivilegedMember, user.TierTrusted, nil},
		{user.TierPrivilegedMember, user.TierMember, nil},
	} {
		t.Run(fmt.Sprintf("%s carrier, %s asked", tc.carrier, tc.asked), func(t *testing.T) {
			f := newCarrierFixture(t, tc.carrier)
			req, _ := f.request(t, tc.asked)
			signatures := len(f.keys.texts)

			sc, err := f.joiner.Join(astral.NewContext(nil), req, f.carry(f.newCarrier()))
			if !errors.Is(err, tc.want) {
				t.Fatalf("join: %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				if n := f.count(user.EntryRequests) + f.count(user.EntryContracts); n != 0 || len(f.keys.texts) != signatures {
					t.Fatalf("stored %d entries and signed %d", n, len(f.keys.texts)-signatures)
				}
				assertJoinEnded(t, f)
				return
			}
			if m, ok := user.MembershipOf(sc); !ok || m.Tier != tc.asked {
				t.Fatalf("membership %v", sc)
			}
		})
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
		t.Run(name, func(t *testing.T) {
			f := newCarrierFixture(t, user.TierPrivilegedMember)
			req, _ := f.request(t, user.TierMember)
			_, err := f.newCarrier().Carry(astral.NewContext(nil), req, nil, func(ctx *astral.Context, half *auth.SignedContract) (*auth.SignedContract, error) {
				signed, err := f.joiner.cosign(ctx, req, half)
				if err != nil {
					return nil, err
				}
				return tamper(signed), nil
			})
			if !errors.Is(err, errNotSignedByJoiner) {
				t.Fatalf("carry: %v, want %v", err, errNotSignedByJoiner)
			}
			if n := f.count(user.EntryContracts); n != 0 {
				t.Fatalf("contracts in SwarmState: %d, want 0", n)
			}
		})
	}
}

// Done: the joiner co-signs only the membership its request asked for.
func TestJoinerSignsOnlyWhatItAskedFor(t *testing.T) {
	f := newCarrierFixture(t, user.TierPrivilegedMember)
	req, reqID := f.request(t, user.TierMember)
	other := astral.GenerateIdentity()
	otherID, _ := astral.ResolveObjectID(&astral.Ack{})
	for name, tc := range map[string]struct {
		issuer, subject *astral.Identity
		tier            user.Tier
		req             *astral.ObjectID
	}{
		"another subject": {f.carrier, other, user.TierMember, reqID},
		"another tier":    {f.carrier, f.joiner.self, user.TierTrusted, reqID},
		"another request": {f.carrier, f.joiner.self, user.TierMember, otherID},
		"another issuer":  {other, f.joiner.self, user.TierMember, reqID},
	} {
		t.Run(name, func(t *testing.T) {
			contract, err := user.NewMembership(tc.issuer, tc.subject, tc.tier, tc.req, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			half := &auth.SignedContract{Contract: contract, IssuerSig: testSig()}
			if _, err = f.joiner.cosign(astral.NewContext(nil), req, half); err == nil {
				t.Fatal("the joiner signed")
			}
		})
	}
}

// Done: a request filed later than the carrier's time is refused.
func TestJoinRefusesARequestFiledLater(t *testing.T) {
	f := newCarrierFixture(t, user.TierPrivilegedMember)
	req, _ := f.request(t, user.TierMember)
	c := f.newCarrier()
	c.now = func() time.Time { return f.clock.now().Add(-time.Minute) }
	if _, err := c.Carry(astral.NewContext(nil), req, nil, f.cosignAs(req)); !errors.Is(err, errFiledLater) {
		t.Fatalf("carry: %v, want %v", err, errFiledLater)
	}
}

// Done: a Privileged join needs the user key's signature.
func TestJoinPrivilegedNeedsUserKey(t *testing.T) {
	f := newCarrierFixture(t, user.TierPrivilegedMember)
	req, reqID := f.request(t, user.TierPrivilegedMember)
	answerJoin(t, f.swarm, f.keys, f.carrier, reqID, false)

	// an Approval does not stand in for the user key
	if _, err := f.newCarrier().Carry(astral.NewContext(nil), req, nil, f.cosignAs(req)); !errors.Is(err, errUserKeyRequired) {
		t.Fatalf("carry without the user key: %v, want %v", err, errUserKeyRequired)
	}

	// a ceremony that cannot reach the user key signs nothing; a Coldcard's
	// press is the user action, so these ceremonies sign without
	// user.confirm_join
	c := f.newCarrier()
	unreached := newUserKeyCeremony(astral.GenerateIdentity(), f.keys, f.swarm)
	unreached.devicePress = true
	if _, err := c.Carry(astral.NewContext(nil), req, unreached, f.cosignAs(req)); err == nil {
		t.Fatal("ceremony signed without the user key")
	}
	if n := f.count(user.EntryContracts); n != 0 {
		t.Fatalf("contracts in SwarmState: %d, want 0", n)
	}

	// the user key signs, the joiner co-signs, and the text shows the tier
	held := newUserKeyCeremony(f.userID, f.keys, f.swarm)
	held.devicePress = true
	sc, err := c.Carry(astral.NewContext(nil), req, held, f.cosignAs(req))
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

// dialCarryJoin sends a user.carry_join query from caller to mod's OpCarryJoin
// and returns the joiner's end of the channel the carrier session runs on.
func dialCarryJoin(t *testing.T, mod *Module, caller *astral.Identity) *channel.Channel {
	t.Helper()

	op, err := routing.NewOp(mod.OpCarryJoin)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	answers, carrierEnd := io.Pipe()
	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	t.Cleanup(cancel)

	requester, err := op.RouteQuery(ctx, astral.Launch(query.New(caller, mod.node.Identity(), "user.carry_join", nil)), carrierEnd)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	t.Cleanup(func() { requester.Close() })
	return channel.New(struct {
		io.Reader
		io.Writer
		io.Closer
	}{answers, requester, requester})
}

// Done: the carrier session runs its four messages over one channel: the
// request, the membership its issuer signed, the membership the joiner signed,
// and the stored membership with the user identity.
//
// note: the carrier end here is Carry with the op's own cosign, not
// OpCarryJoin: the op's UntilDisconnect watch reads the connection the joiner
// answers on, so a session that waits for the joiner's signature cannot run
// through it.
func TestCarrierSessionOverTheWire(t *testing.T) {
	f := newCarrierFixture(t, user.TierTrusted)
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
		sc, err := f.newCarrier().Carry(astral.NewContext(nil), got, nil, func(_ *astral.Context, half *auth.SignedContract) (*auth.SignedContract, error) {
			if err := ch.Send(half); err != nil {
				return nil, err
			}
			var signed *auth.SignedContract
			if err := ch.Switch(channel.Expect(&signed), channel.FailOnEOF()); err != nil {
				return nil, errNotSignedByJoiner
			}
			return signed, nil
		})
		if err != nil {
			carried <- err
			return
		}
		if err = ch.Send(sc); err != nil {
			carried <- err
			return
		}
		carried <- ch.Send(f.userID)
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
	f := newCarrierFixture(t, user.TierTrusted)
	f.swarm.carrier = f.newCarrier()
	req, _ := f.request(t, user.TierTrusted)

	_, _, err := joinSession(astral.NewContext(nil), dialCarryJoin(t, f.swarm, req.Joiner), req, f.cosignAs(req))
	if !errors.Is(err, errTierAboveIssuer) {
		t.Fatalf("join session: %v, want %v", err, errTierAboveIssuer)
	}
}
