package auth

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

// swarmWorld is the app grant of the two-signature model: a handler allows
// the user, the user signs the Laptop (Privileged) the permit with Delegation
// 2, the Laptop issues the Pi (Member) the requested permit with Delegation 1,
// and the Pi issues it to App A. The user module's veto of expelled identities
// (isBanned) is modelled by expelled.
type swarmWorld struct {
	mod                 *Module
	expelled            map[string]bool
	user, laptop, phone *astral.Identity
	pi, member, appA    *astral.Identity
	appB                *astral.Identity
}

func newSwarmWorld(t *testing.T) *swarmWorld {
	t.Helper()
	w := &swarmWorld{
		mod:      testModule(t),
		expelled: map[string]bool{},
		user:     astral.GenerateIdentity(), laptop: astral.GenerateIdentity(), phone: astral.GenerateIdentity(),
		pi: astral.GenerateIdentity(), member: astral.GenerateIdentity(),
		appA: astral.GenerateIdentity(), appB: astral.GenerateIdentity(),
	}
	allowRoot(w.mod, w.user)
	seedContract(t, w.mod, w.user, w.laptop, permit(2))
	seedContract(t, w.mod, w.laptop, w.pi, permit(1))
	seedContract(t, w.mod, w.pi, w.appA, permit(0))

	w.mod.AddVeto(func(id *astral.Identity) bool { return w.expelled[id.String()] })
	return w
}

func (w *swarmWorld) expel(id *astral.Identity) { w.expelled[id.String()] = true }

// TestSwarmWalkAllowsAnAppsChain: App A ◄ Pi ◄ Laptop ◄ user. Without the
// user's contract the same index allows nothing: no handler allows the Laptop,
// whatever membership it holds.
func TestSwarmWalkAllowsAnAppsChain(t *testing.T) {
	ctx := astral.NewContext(nil)
	w := newSwarmWorld(t)
	if !w.mod.Authorize(ctx, action(w.appA)) {
		t.Fatal("App A: want allow through Pi and Laptop")
	}

	bare := testModule(t)
	allowRoot(bare, w.user)
	seedContract(t, bare, w.laptop, w.pi, permit(1))
	seedContract(t, bare, w.pi, w.appA, permit(0))
	if bare.Authorize(ctx, action(w.appA)) {
		t.Fatal("App A without the user's contract: want deny")
	}
}

// TestSwarmWalkContinuesOnlyAtTheIssuer: a contract's link to its request
// adds no authority. A Member that holds nothing issues App B the permit with
// a RequestID; the walk continues at that Member, not at any approver, and
// denies. An issuer granted no Delegation cannot pass the permit on either.
//
// note: ported from the Approval-link tests (a reused, mismatched or
// Member-signed Approval); the walk reads no Approval since the
// two-signature model, so who may answer a request is the merge check's.
func TestSwarmWalkContinuesOnlyAtTheIssuer(t *testing.T) {
	ctx := astral.NewContext(nil)
	w := newSwarmWorld(t)
	requestID, err := astral.ResolveObjectID(&astral.Ack{})
	if err != nil {
		t.Fatal(err)
	}
	seedLinkedContract(t, w.mod, w.member, w.appB, requestID, permit(0))
	if w.mod.Authorize(ctx, action(w.appB)) {
		t.Fatal("contract from a Member that holds nothing: want deny")
	}

	shallow := newSwarmWorld(t)
	other := astral.GenerateIdentity()
	seedContract(t, shallow.mod, shallow.laptop, shallow.member, permit(0))
	seedContract(t, shallow.mod, shallow.member, other, permit(0))
	if !shallow.mod.Authorize(ctx, action(shallow.member)) {
		t.Fatal("the Member's own permit: want allow")
	}
	if shallow.mod.Authorize(ctx, action(other)) {
		t.Fatal("a permit passed on without Delegation: want deny")
	}
}

// TestSwarmWalkStopsAtAnExpelledParty: expelling, or vetoing, the host, the
// app or the approver cuts a chain that held before.
//
// note: a Decline no longer cuts it: a Decline only closes its request, and a
// contract both parties signed keeps its authority (PR 4 decision Q1).
func TestSwarmWalkStopsAtAnExpelledParty(t *testing.T) {
	ctx := astral.NewContext(nil)
	for name, cut := range map[string]func(w *swarmWorld){
		"expelled host":     func(w *swarmWorld) { w.expel(w.pi) },
		"expelled app":      func(w *swarmWorld) { w.expel(w.appA) },
		"expelled approver": func(w *swarmWorld) { w.expel(w.laptop) },
		"vetoed host": func(w *swarmWorld) {
			w.mod.AddVeto(func(id *astral.Identity) bool { return id.IsEqual(w.pi) })
		},
	} {
		w := newSwarmWorld(t)
		if !w.mod.Authorize(ctx, action(w.appA)) {
			t.Fatalf("%s: want allow before the cut", name)
		}
		cut(w)
		if w.mod.Authorize(ctx, action(w.appA)) {
			t.Fatalf("%s: want deny", name)
		}
	}
}

// keyCrypto verifies a signature whose data is the signer's public key, as
// the swarm entry check's tests do; every other crypto method panics.
type keyCrypto struct{ cryptomod.Module }

func (keyCrypto) Verify(key *crypto.PublicKey, sig *crypto.Signature, _ crypto.SignableTextObject) error {
	if sig == nil || !bytes.Equal(sig.Data, key.Key) {
		return errors.New("bad signature")
	}
	return nil
}

func keySig(id *astral.Identity) *crypto.Signature {
	return &crypto.Signature{Scheme: "test", Data: secp256k1.FromIdentity(id).Key}
}

// TestSwarmWalkNeedsTheSubjectsSignature: a contract its subject did not
// sign, or signed with a wrong key, is not indexed and grants nothing; the
// same contract signed by both parties does.
func TestSwarmWalkNeedsTheSubjectsSignature(t *testing.T) {
	ctx := astral.NewContext(nil)
	for name, c := range map[string]struct {
		subjectSig func(w *swarmWorld) *crypto.Signature
		want       bool
	}{
		"no subject signature":    {func(*swarmWorld) *crypto.Signature { return nil }, false},
		"wrong subject signature": {func(w *swarmWorld) *crypto.Signature { return keySig(w.laptop) }, false},
		"both signatures":         {func(w *swarmWorld) *crypto.Signature { return keySig(w.appB) }, true},
	} {
		w := newSwarmWorld(t)
		w.mod.Crypto = keyCrypto{}
		sc := &auth.SignedContract{
			Contract: &auth.Contract{
				Issuer: w.laptop, Subject: w.appB, Permits: []*auth.Permit{permit(0)},
				ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
			},
			IssuerSig:  keySig(w.laptop),
			SubjectSig: c.subjectSig(w),
		}
		err := w.mod.IndexContract(ctx, sc)
		if (err == nil) != c.want {
			t.Fatalf("%s: IndexContract err = %v, want indexed %v", name, err, c.want)
		}
		if got := w.mod.Authorize(ctx, action(w.appB)); got != c.want {
			t.Fatalf("%s: Authorize = %v, want %v", name, got, c.want)
		}
	}
}

func seedLinkedContract(t *testing.T, mod *Module, issuer, subject *astral.Identity, requestID *astral.ObjectID, permits ...*auth.Permit) {
	t.Helper()
	sc := &auth.SignedContract{
		Contract: &auth.Contract{
			Issuer:    issuer,
			Subject:   subject,
			Permits:   permits,
			ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
			RequestID: requestID,
		},
		IssuerSig:  dummySig(),
		SubjectSig: dummySig(),
	}
	if err := mod.db.storeSignedContract(sc); err != nil {
		t.Fatalf("store contract: %v", err)
	}
}

// TestContractIndexKeepsTheRequestID: the index rebuilds contracts from its
// rows, so a contract's RequestID, part of its signed body, survives it, and
// a contract with none stays without one.
func TestContractIndexKeepsTheRequestID(t *testing.T) {
	ctx := astral.NewContext(nil)
	mod := testModule(t)
	issuer, linked, plain := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	requestID, err := astral.ResolveObjectID(&astral.Ack{})
	if err != nil {
		t.Fatal(err)
	}
	seedLinkedContract(t, mod, issuer, linked, requestID, permit(0))
	seedContract(t, mod, issuer, plain, permit(0))

	got, err := mod.SignedContracts().WithSubject(linked).Find(ctx)
	if err != nil || len(got) != 1 || !got[0].RequestID.IsEqual(requestID) {
		t.Fatalf("linked contract: want RequestID %v, got %v (err %v)", requestID, got, err)
	}
	got, err = mod.SignedContracts().WithSubject(plain).Find(ctx)
	if err != nil || len(got) != 1 || got[0].RequestID != nil {
		t.Fatalf("plain contract: want no RequestID, got %v (err %v)", got, err)
	}
}

// TestSwarmWalkGivesAJoinerNoPermitItsCarrierLacks: a Trusted carrier signs
// its Member joiner a membership with extra permits. The walk continues at
// the carrier, which holds none of them, so the joiner neither relays as the
// user, nor administers the swarm, nor takes any other extra permit.
func TestSwarmWalkGivesAJoinerNoPermitItsCarrierLacks(t *testing.T) {
	ctx := astral.NewContext(nil)
	w := newSwarmWorld(t)
	joiner := astral.GenerateIdentity()
	tier := &auth.Permit{Action: astral.String8(user.TierMember.Action())}
	seedContract(t, w.mod, w.phone, joiner, tier,
		&auth.Permit{Action: astral.String8(nodes.RelayForAction{}.ObjectType())},
		&auth.Permit{Action: astral.String8(user.AdminSwarmAction{}.ObjectType())},
		permit(0))

	if w.mod.Authorize(ctx, &nodes.RelayForAction{Action: auth.NewAction(joiner), ForID: w.user}) {
		t.Fatal("member joiner relays as the user: want deny")
	}
	if w.mod.Authorize(ctx, &user.AdminSwarmAction{Action: auth.NewAction(joiner)}) {
		t.Fatal("member joiner administers the swarm: want deny")
	}
	if w.mod.Authorize(ctx, action(joiner)) {
		t.Fatal("member joiner takes an extra permit: want deny")
	}
}

// userPowerWorld registers the user power handlers as mod/user does: expel and
// AdminSwarm for the user, AdminSwarm NodeLocal for the Privileged Laptop, and
// the module's own sudo rule. Relaying registers as mod/nodes and mod/user do:
// an identity relays for itself, and the Laptop, NodeLocal, for the user.
func userPowerWorld(t *testing.T) *swarmWorld {
	t.Helper()
	w := newSwarmWorld(t)
	w.mod.Add(authmod.Func[*auth.SudoAction](w.mod.AuthorizeSudo))
	w.mod.Add(authmod.Func[*user.ExpelAction](func(_ *astral.Context, a *user.ExpelAction) bool {
		return a.Actor().IsEqual(w.user)
	}))
	w.mod.Add(authmod.Func[*user.AdminSwarmAction](func(_ *astral.Context, a *user.AdminSwarmAction) bool {
		return a.Actor().IsEqual(w.user)
	}))
	w.mod.Add(authmod.NodeLocal(authmod.Func[*user.AdminSwarmAction](func(_ *astral.Context, a *user.AdminSwarmAction) bool {
		return a.Actor().IsEqual(w.laptop)
	})))
	w.mod.Add(authmod.Func[*nodes.RelayForAction](func(_ *astral.Context, a *nodes.RelayForAction) bool {
		return a.Actor().IsEqual(a.ForID)
	}))
	w.mod.Add(authmod.NodeLocal(authmod.Func[*nodes.RelayForAction](func(_ *astral.Context, a *nodes.RelayForAction) bool {
		return a.ForID.IsEqual(w.user) && a.Actor().IsEqual(w.laptop)
	})))
	return w
}

// TestRelayAsTheUserDelegatesOnlyFromTheUser: the Privileged Laptop relays as
// the user itself, and a Member holding the Laptop's relay contract does not
// (D6). The same permit in a contract the user signs carries.
func TestRelayAsTheUserDelegatesOnlyFromTheUser(t *testing.T) {
	ctx := astral.NewContext(nil)
	w := userPowerWorld(t)
	relay := func(actor *astral.Identity) bool {
		return w.mod.Authorize(ctx, &nodes.RelayForAction{Action: auth.NewAction(actor), ForID: w.user})
	}
	relayPermit := func(delegation uint8) *auth.Permit {
		return &auth.Permit{Action: astral.String8(nodes.RelayForAction{}.ObjectType()), Delegation: astral.Uint8(delegation)}
	}

	if !relay(w.user) {
		t.Fatal("the user: want allow")
	}
	if !relay(w.laptop) {
		t.Fatal("privileged node relays as the user itself: want allow")
	}

	seedContract(t, w.mod, w.laptop, w.pi, relayPermit(1))
	seedContract(t, w.mod, w.pi, w.appA, relayPermit(0))
	for name, actor := range map[string]*astral.Identity{"member with the privileged node's contract": w.pi, "two hops": w.appA} {
		if relay(actor) {
			t.Fatalf("%s: want deny", name)
		}
	}

	seedContract(t, w.mod, w.user, w.member, relayPermit(1))
	seedContract(t, w.mod, w.member, w.appB, relayPermit(0))
	for name, actor := range map[string]*astral.Identity{"the user's subject": w.member, "two hops": w.appB} {
		if !relay(actor) {
			t.Fatalf("%s: want allow through the user's contract", name)
		}
	}
}

// TestUserPowersDelegateOnlyFromTheirHandlers: a Privileged node administers
// the swarm itself, and passes neither that nor an expel nor acting as the
// user on through a contract it signs. The same permits in a contract the
// user signs carry, and nothing allows a Privileged node an expel or acting
// as the user itself.
func TestUserPowersDelegateOnlyFromTheirHandlers(t *testing.T) {
	ctx := astral.NewContext(nil)
	w := userPowerWorld(t)
	adminSwarm := func(actor *astral.Identity) bool {
		return w.mod.Authorize(ctx, &user.AdminSwarmAction{Action: auth.NewAction(actor)})
	}
	expel := func(actor *astral.Identity) bool {
		return w.mod.Authorize(ctx, &user.ExpelAction{Action: auth.NewAction(actor), Subject: w.member})
	}
	sudo := func(actor *astral.Identity) bool {
		return w.mod.Authorize(ctx, &auth.SudoAction{Action: auth.NewAction(actor), AsID: w.user})
	}
	powers := func(delegation uint8) []*auth.Permit {
		var ps []*auth.Permit
		for _, a := range []string{user.AdminSwarmAction{}.ObjectType(), user.ExpelAction{}.ObjectType(), auth.SudoAction{}.ObjectType()} {
			ps = append(ps, &auth.Permit{Action: astral.String8(a), Delegation: astral.Uint8(delegation)})
		}
		return ps
	}

	if !adminSwarm(w.user) || !expel(w.user) || !sudo(w.user) {
		t.Fatal("the user: want every power")
	}
	if !adminSwarm(w.laptop) {
		t.Fatal("privileged node administers the swarm itself: want allow")
	}
	if expel(w.laptop) || sudo(w.laptop) {
		t.Fatal("privileged node expels or acts as the user: want deny")
	}

	seedContract(t, w.mod, w.laptop, w.pi, powers(1)...)
	seedContract(t, w.mod, w.pi, w.appA, powers(0)...)
	for name, actor := range map[string]*astral.Identity{"privileged node's subject": w.pi, "two hops": w.appA} {
		if adminSwarm(actor) || expel(actor) || sudo(actor) {
			t.Fatalf("%s: want no user power", name)
		}
	}

	seedContract(t, w.mod, w.user, w.member, powers(1)...)
	seedContract(t, w.mod, w.member, w.appB, powers(0)...)
	for name, actor := range map[string]*astral.Identity{"the user's subject": w.member, "two hops": w.appB} {
		if !adminSwarm(actor) || !expel(actor) || !sudo(actor) {
			t.Fatalf("%s: want every power the user's contract carries", name)
		}
	}

	w.expel(w.member)
	if adminSwarm(w.appB) || expel(w.appB) || sudo(w.appB) {
		t.Fatal("chain through an expelled subject: want deny")
	}
}
