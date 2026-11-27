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

// fakeSwarm is the swarm the walk reads (swarmReader): the root key, the
// memberships it holds and the expelled identities.
type fakeSwarm struct {
	root     *astral.Identity
	expelled map[string]bool
	members  map[string]*user.Membership
}

func newFakeSwarm() *fakeSwarm {
	return &fakeSwarm{expelled: map[string]bool{}, members: map[string]*user.Membership{}}
}

func (f *fakeSwarm) setMember(issuer, subject *astral.Identity, tier user.Tier) {
	c := &auth.Contract{Issuer: issuer, Subject: subject, Permits: []*auth.Permit{{Action: astral.String8(tier.Action())}}}
	sc := &auth.SignedContract{Contract: c, IssuerSig: dummySig(), SubjectSig: dummySig()}
	f.members[subject.String()] = &user.Membership{SignedContract: sc, Tier: tier}
}

func (f *fakeSwarm) expel(id *astral.Identity) { f.expelled[id.String()] = true }

func (f *fakeSwarm) Expelled(id *astral.Identity) bool { return f.expelled[id.String()] }

func (f *fakeSwarm) Membership(id *astral.Identity) (*user.Membership, bool) {
	m, ok := f.members[id.String()]
	if !ok || f.Expelled(id) || f.Expelled(m.Issuer) {
		return nil, false
	}
	return m, true
}

// swarmWorld is the app grant of the two-signature model: the user signs the
// Laptop's PrivilegedMembership, the Laptop issues the Pi (Member) the
// requested permit with Delegation 1, and the Pi issues it to App A. The user
// module's veto of expelled identities (isBanned) is modelled on the fake.
type swarmWorld struct {
	mod                 *Module
	swarm               *fakeSwarm
	user, laptop, phone *astral.Identity
	pi, member, appA    *astral.Identity
	appB                *astral.Identity
}

func newSwarmWorld(t *testing.T) *swarmWorld {
	t.Helper()
	w := &swarmWorld{
		mod:   testModule(t),
		swarm: newFakeSwarm(),
		user:  astral.GenerateIdentity(), laptop: astral.GenerateIdentity(), phone: astral.GenerateIdentity(),
		pi: astral.GenerateIdentity(), member: astral.GenerateIdentity(),
		appA: astral.GenerateIdentity(), appB: astral.GenerateIdentity(),
	}
	w.swarm.root = w.user
	w.swarm.setMember(w.user, w.laptop, user.TierPrivilegedMember)
	w.swarm.setMember(w.user, w.phone, user.TierTrusted)
	w.swarm.setMember(w.laptop, w.pi, user.TierMember)
	w.swarm.setMember(w.laptop, w.member, user.TierMember)

	seedContract(t, w.mod, w.laptop, w.pi, permit(1))
	seedContract(t, w.mod, w.pi, w.appA, permit(0))

	useSwarm(w.mod, w.swarm, w.user, astral.GenerateIdentity())
	w.mod.AddVeto(w.swarm.Expelled)
	return w
}

// useSwarm makes swarm the user module of mod on node, claimed by user, or
// unclaimed when user is nil.
func useSwarm(mod *Module, swarm *fakeSwarm, user, node *astral.Identity) {
	c := readyClaim(user)
	c.fakeSwarm = swarm
	mod.User = c
	mod.node = &identityNode{id: node}
}

// TestSwarmWalkAllowsAnAppsChain: App A ◄ Pi ◄ Laptop ◄ user. Without the
// swarm the same index allows nothing: no handler allows the Laptop.
func TestSwarmWalkAllowsAnAppsChain(t *testing.T) {
	w := newSwarmWorld(t)
	ctx := astral.NewContext(nil)

	if !w.mod.Authorize(ctx, action(w.appA)) {
		t.Fatal("App A: want allow through Pi and Laptop")
	}

	w.mod.User = nil
	if w.mod.Authorize(ctx, action(w.appA)) {
		t.Fatal("App A without the swarm: want deny")
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
		"expelled host":     func(w *swarmWorld) { w.swarm.expel(w.pi) },
		"expelled app":      func(w *swarmWorld) { w.swarm.expel(w.appA) },
		"expelled approver": func(w *swarmWorld) { w.swarm.expel(w.laptop) },
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

func seedLinkedContract(t *testing.T, mod *Module, issuer, subject *astral.Identity, approvalID *astral.ObjectID, permits ...*auth.Permit) {
	t.Helper()
	sc := &auth.SignedContract{
		Contract: &auth.Contract{
			Issuer:     issuer,
			Subject:    subject,
			Permits:    permits,
			ExpiresAt:  astral.Time(time.Now().Add(time.Hour)),
			ApprovalID: approvalID,
		},
		IssuerSig:  dummySig(),
		SubjectSig: dummySig(),
	}
	if err := mod.db.storeSignedContract(sc); err != nil {
		t.Fatalf("store contract: %v", err)
	}
}

// TestSwarmRootAllowsAtAnyHopButNeverAnExpel: the root key and a holder of a
// root-signed PrivilegedMembership are allowed directly and at the top of a
// plain delegation chain two hops up, but never asked about an expel.
func TestSwarmRootAllowsAtAnyHopButNeverAnExpel(t *testing.T) {
	ctx := astral.NewContext(nil)
	w := newSwarmWorld(t)
	mid, leaf := astral.GenerateIdentity(), astral.GenerateIdentity()
	seedContract(t, w.mod, w.laptop, mid, permit(1))
	seedContract(t, w.mod, mid, leaf, permit(0))

	for name, actor := range map[string]*astral.Identity{
		"user key, hop 0": w.user,
		"laptop, hop 0":   w.laptop,
		"mid, hop 1":      mid,
		"leaf, hop 2":     leaf,
	} {
		if !w.mod.Authorize(ctx, action(actor)) {
			t.Fatalf("%s: want allow", name)
		}
	}

	for name, actor := range map[string]*astral.Identity{"user key": w.user, "laptop": w.laptop} {
		if w.mod.Authorize(ctx, &user.ExpelAction{Action: auth.NewAction(actor), Subject: w.pi}) {
			t.Fatalf("%s expel: want deny", name)
		}
		// the user's authority answers requests and administers the swarm
		if !w.mod.Authorize(ctx, &user.AdminSwarmAction{Action: auth.NewAction(actor)}) {
			t.Fatalf("%s admin swarm: want allow", name)
		}
	}

	// a Member, a Trusted node and an expelled Privileged node are not roots
	for name, actor := range map[string]*astral.Identity{"member": w.member, "trusted": w.phone} {
		if w.mod.Authorize(ctx, action(actor)) {
			t.Fatalf("%s: want deny", name)
		}
	}
	w.swarm.expel(w.laptop)
	if w.mod.Authorize(ctx, action(w.laptop)) || w.mod.Authorize(ctx, action(leaf)) {
		t.Fatal("expelled laptop and its chain: want deny")
	}
}

// TestSwarmRootNeitherActsAsTheUserKeyNorRelaysForOthers: the root answers no
// sudo, so a Privileged node acts as no other key (D2, D7), and it relays only
// as the user (D6).
func TestSwarmRootNeitherActsAsTheUserKeyNorRelaysForOthers(t *testing.T) {
	ctx := astral.NewContext(nil)
	w := newSwarmWorld(t)

	sudo := func(actor, as *astral.Identity) bool {
		return w.mod.Authorize(ctx, &auth.SudoAction{Action: auth.NewAction(actor), AsID: as})
	}
	relay := func(actor, forID *astral.Identity) bool {
		return w.mod.Authorize(ctx, &nodes.RelayForAction{Action: auth.NewAction(actor), ForID: forID})
	}

	if sudo(w.laptop, w.user) {
		t.Fatal("privileged sudo as the user key: want deny")
	}
	if sudo(w.laptop, w.appA) {
		t.Fatal("privileged sudo as an app: want deny")
	}
	if sudo(w.user, w.laptop) {
		t.Fatal("the user key as a node: want deny (no root sudo)")
	}
	if !relay(w.laptop, w.user) {
		t.Fatal("privileged relay for the user: want allow")
	}
	for name, forID := range map[string]*astral.Identity{"member node": w.pi, "another node's app": w.appA} {
		if relay(w.laptop, forID) {
			t.Fatalf("privileged relay for %s: want deny", name)
		}
	}
}

// TestSwarmRootRequiresTheRootKeysSignature: a PrivilegedMembership another key
// signed is no root, and an unclaimed node is its own root.
func TestSwarmRootRequiresTheRootKeysSignature(t *testing.T) {
	ctx := astral.NewContext(nil)
	w := newSwarmWorld(t)
	rogue := astral.GenerateIdentity()
	w.swarm.setMember(w.laptop, rogue, user.TierPrivilegedMember)
	if w.mod.Authorize(ctx, action(rogue)) {
		t.Fatal("privileged membership not signed by the root key: want deny")
	}

	node := astral.GenerateIdentity()
	useSwarm(w.mod, w.swarm, nil, node)
	if !w.mod.Authorize(ctx, action(node)) {
		t.Fatal("unclaimed node as its own root: want allow")
	}
	if w.mod.Authorize(ctx, &user.ExpelAction{Action: auth.NewAction(node), Subject: w.pi}) {
		t.Fatal("unclaimed node expel: want deny")
	}
}

// TestContractIndexKeepsTheApprovalID: the walk rebuilds contracts from the
// index, so a link survives it and a contract with none stays unlinked.
func TestContractIndexKeepsTheApprovalID(t *testing.T) {
	ctx := astral.NewContext(nil)
	mod := testModule(t)
	issuer, linked, plain := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	approvalID, err := astral.ResolveObjectID(&astral.Ack{})
	if err != nil {
		t.Fatal(err)
	}
	seedLinkedContract(t, mod, issuer, linked, approvalID, permit(0))
	seedContract(t, mod, issuer, plain, permit(0))

	got, err := mod.SignedContracts().WithSubject(linked).Find(ctx)
	if err != nil || len(got) != 1 || !got[0].ApprovalID.IsEqual(approvalID) {
		t.Fatalf("linked contract: want ApprovalID %v, got %v (err %v)", approvalID, got, err)
	}
	got, err = mod.SignedContracts().WithSubject(plain).Find(ctx)
	if err != nil || len(got) != 1 || got[0].ApprovalID != nil {
		t.Fatalf("plain contract: want no ApprovalID, got %v (err %v)", got, err)
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

// TestSwarmWalkGrantsUserPowersOnlyFromTheRoot: a Privileged node signs a
// Member a contract with the four user powers, and the Member passes relaying
// as the user on to an app. Neither carries relaying as the user, AdminSwarm,
// sudo as the user or an expel, while the Privileged node itself still relays
// as the user and administers the swarm, the Member's other permits, an app's
// relay contract for itself and the user key's own contract still carry.
func TestSwarmWalkGrantsUserPowersOnlyFromTheRoot(t *testing.T) {
	ctx := astral.NewContext(nil)
	w := newSwarmWorld(t)
	w.mod.Add(authmod.Func[*nodes.RelayForAction](func(_ *astral.Context, a *nodes.RelayForAction) bool {
		return a.Actor().IsEqual(a.ForID)
	}))
	relayFor := &auth.Permit{Action: astral.String8(nodes.RelayForAction{}.ObjectType())}
	adminSwarm := &auth.Permit{Action: astral.String8(user.AdminSwarmAction{}.ObjectType())}
	seedContract(t, w.mod, w.laptop, w.pi,
		&auth.Permit{Action: relayFor.Action, Delegation: 1}, adminSwarm,
		&auth.Permit{Action: astral.String8(auth.SudoAction{}.ObjectType())},
		&auth.Permit{Action: astral.String8(user.ExpelAction{}.ObjectType())})
	seedContract(t, w.mod, w.pi, w.appB, relayFor)

	for name, actor := range map[string]*astral.Identity{"member": w.pi, "the member's app": w.appB} {
		if w.mod.Authorize(ctx, &nodes.RelayForAction{Action: auth.NewAction(actor), ForID: w.user}) {
			t.Fatalf("%s relays as the user: want deny", name)
		}
	}
	if w.mod.Authorize(ctx, &user.AdminSwarmAction{Action: auth.NewAction(w.pi)}) {
		t.Fatal("member administers the swarm: want deny")
	}
	if w.mod.Authorize(ctx, &auth.SudoAction{Action: auth.NewAction(w.pi), AsID: w.user}) {
		t.Fatal("member acts as the user key: want deny")
	}
	if w.mod.Authorize(ctx, &user.ExpelAction{Action: auth.NewAction(w.pi), Subject: w.member}) {
		t.Fatal("member expels: want deny")
	}
	if !w.mod.Authorize(ctx, action(w.pi)) {
		t.Fatal("member's other permit: want allow")
	}

	if !w.mod.Authorize(ctx, &nodes.RelayForAction{Action: auth.NewAction(w.laptop), ForID: w.user}) {
		t.Fatal("privileged node relays as the user itself: want allow")
	}
	if !w.mod.Authorize(ctx, &user.AdminSwarmAction{Action: auth.NewAction(w.laptop)}) {
		t.Fatal("privileged node administers the swarm itself: want allow")
	}

	seedContract(t, w.mod, w.appA, w.pi, relayFor)
	if !w.mod.Authorize(ctx, &nodes.RelayForAction{Action: auth.NewAction(w.pi), ForID: w.appA}) {
		t.Fatal("member relays for an app by the app's own contract: want allow")
	}

	seedContract(t, w.mod, w.user, w.member, adminSwarm)
	if !w.mod.Authorize(ctx, &user.AdminSwarmAction{Action: auth.NewAction(w.member)}) {
		t.Fatal("the user key's own contract: want allow")
	}
}
