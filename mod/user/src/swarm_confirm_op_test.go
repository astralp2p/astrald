package user

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// softwareKeyFixture is a carrier fixture whose user key is a software key
// held on the carrier: a ceremony with no device press, the carrier using it,
// and the ops serving user.confirm_join with it.
type softwareKeyFixture struct {
	*carrierFixture
	ceremony  *userKeyCeremony
	ops       *SwarmAnswerOps
	confirmed *joinConfirmations
}

func newSoftwareKeyFixture() *softwareKeyFixture {
	f := &softwareKeyFixture{carrierFixture: newCarrierFixture(user.TierPrivilegedMember)}
	f.ceremony = newUserKeyCeremony(f.userID, f.keys, f.swarm, f.swarm)
	f.ceremony.now = f.clock.now
	f.ops = &SwarmAnswerOps{
		Node:    f.carrier,
		State:   writeState{entryReader: f.swarm, swarmWriter: f.newWriter()},
		View:    f.swarm,
		Auth:    &recordingAuth{verdict: true},
		UserKey: func() *userKeyCeremony { return f.ceremony },
	}
	f.confirmed = &joinConfirmations{}
	f.ops.Confirmed = f.confirmed
	return f
}

func (f *softwareKeyFixture) carrierWithKey() *swarmCarrier {
	c := f.newCarrier()
	c.ceremony = func() *userKeyCeremony { return f.ceremony }
	c.confirmed = f.confirmed
	return c
}

// confirm calls user.confirm_join on id as caller and returns what it sent.
func (f *softwareKeyFixture) confirm(t *testing.T, caller *astral.Identity, id *astral.ObjectID) astral.Object {
	t.Helper()
	af := &answerFixture{ops: f.ops, caller: caller}
	objs := af.run(t, f.ops.OpConfirmJoin, "user.confirm_join?id="+id.String())
	if len(objs) != 1 {
		t.Fatalf("confirm_join sent %d objects; want 1", len(objs))
	}
	return objs[0]
}

// Done: with a software user key, a privileged JoinRequest alone yields no
// PrivilegedMembership; the user's user.confirm_join signs it as issuer and
// adds nothing to SwarmState, the carrier hands it to the joiner, and the
// joiner co-signs it in the carrier session.
func TestPrivilegedJoinWaitsForUserConfirmation(t *testing.T) {
	f := newSoftwareKeyFixture()
	req, reqID := f.request(t, user.TierPrivilegedMember)

	done := f.startJoin(t, astral.NewContext(nil), req, f.carrierWithKey())

	// the carrier polls many times and signs nothing
	time.Sleep(50 * time.Millisecond)
	select {
	case r := <-done:
		t.Fatalf("join ended before the user confirmed: %v", r.err)
	default:
	}
	if n := f.swarm.count(user.EntryContracts); n != 0 {
		t.Fatalf("contracts in SwarmState before confirmation: %d, want 0", n)
	}
	if n := len(f.keys.texts); n != 1 {
		t.Fatalf("signatures before confirmation: %d, want only the joiner's request", n)
	}

	// the request is listed for the user to confirm
	af := &answerFixture{ops: f.ops, caller: f.userID}
	listed := af.run(t, f.ops.OpSwarmRequests, "user.swarm_requests")
	if len(listed) != 4 || !listed[0].(*astral.ObjectID).IsEqual(reqID) {
		t.Fatalf("swarm_requests sent %v; want the privileged request", listed)
	}

	sc, ok := f.confirm(t, f.userID, reqID).(*auth.SignedContract)
	if !ok {
		t.Fatal("confirm_join sent no membership")
	}
	assertUserKeyMembership(t, sc, f.userID, req.Joiner, reqID)
	verifySignedBy(t, f.userID, sc.Contract, sc.IssuerSig)
	if sc.SubjectSig != nil {
		t.Fatal("confirm_join sent a membership the joiner signed")
	}

	r := receiveJoin(t, done)
	if r.err != nil {
		t.Fatalf("join: %v", r.err)
	}
	if !r.sc.Contract.Issuer.IsEqual(f.userID) || !r.sc.Subject.IsEqual(req.Joiner) || !sameBody(sc.Contract, r.sc.Contract) {
		t.Fatalf("joiner got %v -> %v", r.sc.Issuer, r.sc.Subject)
	}
	verifySignedBy(t, req.Joiner, r.sc.Contract, r.sc.SubjectSig)

	// a repeated confirmation signs nothing new
	if _, ok = f.confirm(t, f.userID, reqID).(*auth.SignedContract); !ok {
		t.Fatal("a repeated confirmation failed")
	}
	if n := f.swarm.count(user.EntryContracts); n != 1 {
		t.Fatalf("contracts in SwarmState: %d, want 1", n)
	}
}

// Done: a caller other than the user cannot confirm, whatever its grants.
func TestConfirmJoinRefusesNonUser(t *testing.T) {
	f := newSoftwareKeyFixture()
	req, reqID := f.request(t, user.TierPrivilegedMember)
	if err := f.swarm.merge(astral.NewContext(nil), req); err != nil {
		t.Fatalf("store request: %v", err)
	}

	for name, caller := range map[string]*astral.Identity{
		"carrier": f.carrier,
		"joiner":  f.joiner.self,
		"other":   astral.GenerateIdentity(),
	} {
		got, ok := f.confirm(t, caller, reqID).(astral.Error)
		if !ok || got.Error() != auth.ErrNotPermitted.Error() {
			t.Fatalf("%s confirmed: %v", name, got)
		}
	}
	if n := f.swarm.count(user.EntryContracts); n != 0 {
		t.Fatalf("contracts in SwarmState: %d, want 0", n)
	}
}

// Done: a query with the user as caller that is not local cannot confirm. A
// member relaying as the user arrives with the user as caller and a network
// origin.
func TestConfirmJoinRefusesNonLocalOrigin(t *testing.T) {
	f := newSoftwareKeyFixture()
	req, reqID := f.request(t, user.TierPrivilegedMember)
	if err := f.swarm.merge(astral.NewContext(nil), req); err != nil {
		t.Fatalf("store request: %v", err)
	}
	signatures := len(f.keys.texts)

	for _, origin := range []string{astral.OriginNetwork, astral.OriginMCP} {
		af := &answerFixture{ops: f.ops, caller: f.userID, origin: origin}
		objs := af.run(t, f.ops.OpConfirmJoin, "user.confirm_join?id="+reqID.String())
		if len(objs) != 1 {
			t.Fatalf("%s: confirm_join sent %d objects; want 1", origin, len(objs))
		}
		got, ok := objs[0].(astral.Error)
		if !ok || got.Error() != astral.NewErrOriginNotAllowed(origin).Error() {
			t.Fatalf("%s origin confirmed: %v", origin, objs[0])
		}
	}
	if n := f.swarm.count(user.EntryContracts); n != 0 {
		t.Fatalf("contracts in SwarmState: %d, want 0", n)
	}
	if n := len(f.keys.texts); n != signatures {
		t.Fatalf("signatures: %d, want %d", n, signatures)
	}

	// the same caller from a local origin confirms
	af := &answerFixture{ops: f.ops, caller: f.userID, origin: astral.OriginLocal}
	objs := af.run(t, f.ops.OpConfirmJoin, "user.confirm_join?id="+reqID.String())
	if len(objs) != 1 {
		t.Fatalf("local: confirm_join sent %d objects; want 1", len(objs))
	}
	if _, ok := objs[0].(*auth.SignedContract); !ok {
		t.Fatalf("local origin: confirm_join sent %v; want a membership", objs[0])
	}
}

// user.confirm_join refuses what is not an open privileged join, and a node
// that cannot reach the user key.
func TestConfirmJoinRefusals(t *testing.T) {
	f := newSoftwareKeyFixture()
	store := func(req *user.SignedJoinRequest) *astral.ObjectID {
		if err := f.swarm.merge(astral.NewContext(nil), req); err != nil {
			t.Fatalf("store request: %v", err)
		}
		id, _ := astral.ResolveObjectID(req)
		return id
	}
	refused := func(id *astral.ObjectID, want astral.Error) {
		t.Helper()
		got, ok := f.confirm(t, f.userID, id).(astral.Error)
		if !ok || got.Error() != want.Error() {
			t.Fatalf("confirm_join sent %v; want %v", got, want)
		}
	}

	unstored, _ := astral.ResolveObjectID(&user.SignedJoinRequest{
		JoinRequest: user.NewJoinRequest(astral.GenerateIdentity(), astral.GenerateIdentity(), user.TierPrivilegedMember),
		JoinerSig:   testSig(),
	})
	refused(unstored, ErrUnknownRequest)

	member, _ := f.request(t, user.TierMember)
	refused(store(member), ErrNotPrivilegedJoin)

	f.joiner.kept = nil
	declined, declinedID := f.request(t, user.TierPrivilegedMember)
	store(declined)
	answerJoin(t, f.swarm, f.keys, f.userID, declinedID, true)
	refused(declinedID, ErrRequestClosed)

	f.joiner.kept = nil
	f.clock.advance(time.Minute)
	expired, expiredID := f.request(t, user.TierPrivilegedMember)
	store(expired)
	f.clock.advance(2 * user.RequestTimeout)
	refused(expiredID, ErrRequestClosed)

	f.ops.UserKey = func() *userKeyCeremony { return nil }
	refused(expiredID, ErrNoUserKey)

	if n := f.swarm.count(user.EntryContracts); n != 0 {
		t.Fatalf("contracts in SwarmState: %d, want 0", n)
	}
}

// Done: the bootstrap path runs the claim rule; a refused claim signs nothing.
func TestBootstrapPrivilegedClaimChecksClaimRule(t *testing.T) {
	f := newBootstrapFixture()
	f.auth.claimable = false
	key := newJoinKeyring()
	userID := key.newKey()

	sc, err := f.mod.signPrivilegedClaim(astral.NewContext(nil), f.nodeID, key.key(userID))
	if !errors.Is(err, errClaimRefused) || sc != nil {
		t.Fatalf("sign: %v %v; want %v", sc, err, errClaimRefused)
	}
}
