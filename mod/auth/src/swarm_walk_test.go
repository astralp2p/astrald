package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// fakeSwarm is the swarm the walk reads (swarmReader) over an in-memory set of entries.
// Its RequestStatus applies the G0 precedence: a Decline, or an expel of the
// request's filer, carrier or subject, beats an Approval; answers by expelled
// signers do not count.
type fakeSwarm struct {
	root     *astral.Identity // the root key MayAnswer reads
	entries  map[string]astral.Object
	expelled map[string]bool
	members  map[string]*user.Membership
}

func newFakeSwarm() *fakeSwarm {
	return &fakeSwarm{
		entries:  map[string]astral.Object{},
		expelled: map[string]bool{},
		members:  map[string]*user.Membership{},
	}
}

func (f *fakeSwarm) add(t *testing.T, obj astral.Object) *astral.ObjectID {
	t.Helper()
	id, err := astral.ResolveObjectID(obj)
	if err != nil {
		t.Fatalf("resolve entry id: %v", err)
	}
	f.entries[id.String()] = obj
	return id
}

func (f *fakeSwarm) setMember(issuer, subject *astral.Identity, tier user.Tier) {
	c := &auth.Contract{Issuer: issuer, Subject: subject, Permits: []*auth.Permit{{Action: astral.String8(tier.Action())}}}
	f.members[subject.String()] = &user.Membership{SignedContract: &auth.SignedContract{Contract: c, IssuerSig: dummySig()}, Tier: tier}
}

func (f *fakeSwarm) expel(id *astral.Identity) { f.expelled[id.String()] = true }

// SwarmState

func (f *fakeSwarm) SwarmEntry(id *astral.ObjectID) (astral.Object, error) {
	if obj, ok := f.entries[id.String()]; ok {
		return obj, nil
	}
	return nil, errors.New("not stored")
}

func (f *fakeSwarm) SwarmEntries(user.EntryKind) ([]*astral.ObjectID, error) { return nil, nil }
func (f *fakeSwarm) SwarmHead() uint64                                       { return uint64(len(f.entries)) }

// SwarmView

func (f *fakeSwarm) RequestStatus(req *astral.ObjectID) user.RequestStatus {
	obj := f.entries[req.String()]
	var parties []*astral.Identity
	switch r := obj.(type) {
	case *user.SignedContractRequest:
		parties = []*astral.Identity{r.FiledBy, r.Identity}
	case *user.SignedJoinRequest:
		parties = []*astral.Identity{r.Carrier, r.Joiner}
	}
	for _, p := range parties {
		if f.Expelled(p) {
			return user.RequestDeclined
		}
	}

	approved := false
	for _, e := range f.entries {
		switch a := e.(type) {
		case *user.SignedDecline:
			if a.RequestID.IsEqual(req) && !f.Expelled(a.Issuer) {
				return user.RequestDeclined
			}
		case *user.SignedApproval:
			if a.RequestID.IsEqual(req) && !f.Expelled(a.Issuer) {
				approved = true
			}
		}
	}
	if approved {
		return user.RequestApproved
	}
	return user.RequestPending
}

func (f *fakeSwarm) Expelled(id *astral.Identity) bool { return f.expelled[id.String()] }

func (f *fakeSwarm) ApprovalsTouching(*astral.Identity) []*user.SignedApproval { return nil }

func (f *fakeSwarm) CarriedOut(*astral.ObjectID) []*auth.SignedContract { return nil }

func (f *fakeSwarm) Membership(id *astral.Identity) (*user.Membership, bool) {
	m, ok := f.members[id.String()]
	if !ok || f.Expelled(id) || f.Expelled(m.Issuer) {
		return nil, false
	}
	return m, true
}

// MayAnswer applies the answer rule: the root answers every request, any
// other signer by its tier.
func (f *fakeSwarm) MayAnswer(signer *astral.Identity, req *astral.ObjectID) bool {
	obj, ok := f.entries[req.String()]
	if !ok {
		return false
	}
	if !f.root.IsZero() && signer.IsEqual(f.root) {
		return true
	}
	m, ok := f.Membership(signer)
	return ok && user.MayAnswer(m.Tier, obj)
}

// swarmWorld is the chain of the vision: the user signs the Laptop's
// PrivilegedMembership, the Pi (Member) files R1 for App A, the Laptop
// approves R1, and the Pi signs Pi → App A carrying out that Approval.
type swarmWorld struct {
	mod                 *Module
	swarm               *fakeSwarm
	user, laptop, phone *astral.Identity
	pi, member, appA    *astral.Identity
	appB                *astral.Identity
	r1, approvalR1      *astral.ObjectID
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

	w.r1 = w.swarm.add(t, &user.SignedContractRequest{
		ContractRequest: user.NewContractRequest(w.pi, w.appA, []*auth.Permit{permit(0)}),
		FiledBySig:      dummySig(),
	})
	w.approvalR1 = w.answer(t, w.laptop, w.r1)
	seedLinkedContract(t, w.mod, w.pi, w.appA, w.approvalR1, permit(0))

	useSwarm(w.mod, w.swarm, w.user, astral.GenerateIdentity())
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

// answer stores signer's Approval of req and returns its entry ID.
func (w *swarmWorld) answer(t *testing.T, signer *astral.Identity, req *astral.ObjectID) *astral.ObjectID {
	t.Helper()
	return w.swarm.add(t, &user.SignedApproval{Approval: user.NewApproval(signer, req), IssuerSig: dummySig()})
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

// TestSwarmWalkAllowsAnAppsChain: App A ◄ Pi ◄ Approval R1 ◄ Laptop ◄ user.
// Without the swarm the same index allows nothing: no handler allows the Pi.
func TestSwarmWalkAllowsAnAppsChain(t *testing.T) {
	w := newSwarmWorld(t)
	ctx := astral.NewContext(nil)

	if !w.mod.Authorize(ctx, action(w.appA)) {
		t.Fatal("App A: want allow through its Approval-linked chain")
	}

	w.mod.User = nil
	if w.mod.Authorize(ctx, action(w.appA)) {
		t.Fatal("App A without the swarm: want deny, as on master")
	}
}

// TestSwarmWalkDeniesAReusedApproval: the Pi signs App B a contract carrying
// out App A's Approval. R1 names App A, so App B's link does not hold.
func TestSwarmWalkDeniesAReusedApproval(t *testing.T) {
	w := newSwarmWorld(t)
	ctx := astral.NewContext(nil)
	seedLinkedContract(t, w.mod, w.pi, w.appB, w.approvalR1, permit(0))

	if w.mod.Authorize(ctx, action(w.appB)) {
		t.Fatal("App B reusing App A's Approval: want deny")
	}
	if !w.mod.Authorize(ctx, action(w.appA)) {
		t.Fatal("App A: want allow")
	}
}

// TestSwarmWalkDeniesALinkThatDoesNotMatchItsRequest: the link holds only for
// the request's filer and the permits it asked for.
func TestSwarmWalkDeniesALinkThatDoesNotMatchItsRequest(t *testing.T) {
	ctx := astral.NewContext(nil)

	w := newSwarmWorld(t)
	if !w.mod.Authorize(ctx, action(w.appA)) {
		t.Fatal("the Pi's own link: want allow")
	}
	// another Member signs App A a contract carrying out the Pi's request
	other := testModule(t)
	seedLinkedContract(t, other, w.member, w.appA, w.approvalR1, permit(0))
	useSwarm(other, w.swarm, w.user, astral.GenerateIdentity())
	if other.Authorize(ctx, action(w.appA)) {
		t.Fatal("issuer that did not file R1: want deny")
	}

	// the Pi signs more than R1 asked for
	wider := testModule(t)
	seedLinkedContract(t, wider, w.pi, w.appA, w.approvalR1, permit(3))
	useSwarm(wider, w.swarm, w.user, astral.GenerateIdentity())
	if wider.Authorize(ctx, action(w.appA)) {
		t.Fatal("permits R1 does not cover: want deny")
	}
}

// TestSwarmWalkDeniesAMemberApprover: a Member's Approval of a ContractRequest
// is no link, and neither is a Trusted node's (D5) or a stranger's; the same
// request approved by a Privileged node is.
//
// why each approver holds the action itself: the denial must come from the
// approver's tier, not from a walk that re-enters at an approver with no
// authority of its own.
func TestSwarmWalkDeniesAMemberApprover(t *testing.T) {
	ctx := astral.NewContext(nil)
	for _, c := range []struct {
		name     string
		approver func(w *swarmWorld) *astral.Identity
		want     bool
	}{
		{"privileged", func(w *swarmWorld) *astral.Identity { return w.laptop }, true},
		{"member", func(w *swarmWorld) *astral.Identity { return w.member }, false},
		{"trusted", func(w *swarmWorld) *astral.Identity { return w.phone }, false},
		{"stranger", func(w *swarmWorld) *astral.Identity { return astral.GenerateIdentity() }, false},
	} {
		w := newSwarmWorld(t)
		approver := c.approver(w)
		if !approver.IsEqual(w.laptop) {
			seedContract(t, w.mod, w.laptop, approver, permit(1))
		}
		if !w.mod.Authorize(ctx, action(approver)) {
			t.Fatalf("%s alone: want allow", c.name)
		}

		app := astral.GenerateIdentity()
		req := w.swarm.add(t, &user.SignedContractRequest{
			ContractRequest: user.NewContractRequest(w.pi, app, []*auth.Permit{permit(0)}),
			FiledBySig:      dummySig(),
		})
		seedLinkedContract(t, w.mod, w.pi, app, w.answer(t, approver, req), permit(0))

		if got := w.mod.Authorize(ctx, action(app)); got != c.want {
			t.Fatalf("%s as approver: got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestSwarmWalkDeniesADeclinedOrExpelledLink: a Decline beats the Approval,
// and an expel of the issuer, the subject or the approver, or a veto of the
// issuer, cuts a chain that held before.
func TestSwarmWalkDeniesADeclinedOrExpelledLink(t *testing.T) {
	ctx := astral.NewContext(nil)
	for name, cut := range map[string]func(t *testing.T, w *swarmWorld){
		"declined": func(t *testing.T, w *swarmWorld) {
			desktop := astral.GenerateIdentity()
			w.swarm.setMember(w.user, desktop, user.TierPrivilegedMember)
			w.swarm.add(t, &user.SignedDecline{Decline: user.NewDecline(desktop, w.r1), IssuerSig: dummySig()})
		},
		"expelled issuer":   func(_ *testing.T, w *swarmWorld) { w.swarm.expel(w.pi) },
		"expelled subject":  func(_ *testing.T, w *swarmWorld) { w.swarm.expel(w.appA) },
		"expelled approver": func(_ *testing.T, w *swarmWorld) { w.swarm.expel(w.laptop) },
		"vetoed issuer": func(_ *testing.T, w *swarmWorld) {
			w.mod.AddVeto(func(id *astral.Identity) bool { return id.IsEqual(w.pi) })
		},
	} {
		w := newSwarmWorld(t)
		if !w.mod.Authorize(ctx, action(w.appA)) {
			t.Fatalf("%s: want allow before the cut", name)
		}
		cut(t, w)
		if w.mod.Authorize(ctx, action(w.appA)) {
			t.Fatalf("%s: want deny", name)
		}
	}
}

// lenientSwarm reports every request approved and keeps expelled members, so
// only the walk's own expel check stands between an expel and the link.
type lenientSwarm struct{ *fakeSwarm }

func (l lenientSwarm) RequestStatus(*astral.ObjectID) user.RequestStatus { return user.RequestApproved }

func (l lenientSwarm) Membership(id *astral.Identity) (*user.Membership, bool) {
	m, ok := l.members[id.String()]
	return m, ok
}

// TestSwarmLinkChecksExpelsItself: the link does not rely on RequestStatus or
// Membership to cut an expelled issuer, subject or approver.
func TestSwarmLinkChecksExpelsItself(t *testing.T) {
	for name, party := range map[string]func(w *swarmWorld) *astral.Identity{
		"issuer":   func(w *swarmWorld) *astral.Identity { return w.pi },
		"subject":  func(w *swarmWorld) *astral.Identity { return w.appA },
		"approver": func(w *swarmWorld) *astral.Identity { return w.laptop },
	} {
		w := newSwarmWorld(t)
		walk := &swarmWalk{view: lenientSwarm{w.swarm}, root: func() *astral.Identity { return w.user }}
		sc := &auth.SignedContract{
			Contract:  &auth.Contract{Issuer: w.pi, Subject: w.appA, Permits: []*auth.Permit{permit(0)}, ApprovalID: w.approvalR1},
			IssuerSig: dummySig(),
		}
		if next, ok := walk.approvalLink(sc); !ok || !next.IsEqual(w.laptop) {
			t.Fatalf("%s: want the link to hold before the expel", name)
		}
		w.swarm.expel(party(w))
		if _, ok := walk.approvalLink(sc); ok {
			t.Fatalf("expelled %s: want no link", name)
		}
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
	for name, actor := range map[string]*astral.Identity{"member": w.pi, "trusted": w.phone} {
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

// TestSwarmLinkCountsATrustedApprovalOfAMemberJoin: a membership the carrier
// signs links to a Trusted node's Approval of a Member-tier JoinRequest, but
// not to a Member's, and not at another tier.
func TestSwarmLinkCountsATrustedApprovalOfAMemberJoin(t *testing.T) {
	w := newSwarmWorld(t)
	joiner := astral.GenerateIdentity()
	join := w.swarm.add(t, &user.SignedJoinRequest{
		JoinRequest: user.NewJoinRequest(joiner, w.phone, user.TierMember),
		JoinerSig:   dummySig(),
	})
	membership := func(approver *astral.Identity, tier user.Tier) *auth.SignedContract {
		c, err := user.NewMembership(w.phone, joiner, tier, w.answer(t, approver, join), time.Hour)
		if err != nil {
			t.Fatalf("membership: %v", err)
		}
		return &auth.SignedContract{Contract: c, IssuerSig: dummySig()}
	}

	if next, ok := w.mod.contractLink(membership(w.phone, user.TierMember)); !ok || !next.IsEqual(w.phone) {
		t.Fatal("trusted approver of a member join: want the link to hold")
	}
	if _, ok := w.mod.contractLink(membership(w.member, user.TierMember)); ok {
		t.Fatal("member approver of a join: want no link")
	}
	if _, ok := w.mod.contractLink(membership(w.laptop, user.TierTrusted)); ok {
		t.Fatal("membership at another tier than the join asked: want no link")
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

// TestSwarmLinkGrantsAJoinerItsTierOnly: a Trusted carrier signs its Member
// joiner a membership with extra permits, carrying out a Privileged node's
// Approval of the Member JoinRequest. The link does not hold, so the joiner
// neither relays as the user, nor administers the swarm, nor takes any other
// extra permit up to the approver; the exact membership still links.
func TestSwarmLinkGrantsAJoinerItsTierOnly(t *testing.T) {
	ctx := astral.NewContext(nil)
	w := newSwarmWorld(t)
	joiner := astral.GenerateIdentity()
	join := w.swarm.add(t, &user.SignedJoinRequest{
		JoinRequest: user.NewJoinRequest(joiner, w.phone, user.TierMember),
		JoinerSig:   dummySig(),
	})
	approval := w.answer(t, w.laptop, join)
	tier := &auth.Permit{Action: astral.String8(user.TierMember.Action())}
	seedLinkedContract(t, w.mod, w.phone, joiner, approval, tier,
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

	exact, err := user.NewMembership(w.phone, joiner, user.TierMember, approval, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := w.mod.contractLink(&auth.SignedContract{Contract: exact, IssuerSig: dummySig()}); !ok {
		t.Fatal("the exact membership: want the link to hold")
	}
}

// TestSwarmWalkCarriesNoUserAuthorityThroughAnUnlinkedContract: a Privileged
// node signs a Member a contract with no ApprovalID. It carries no relay as
// the user, no AdminSwarm, no sudo as the user and no expel, while its other
// permits, an app's relay contract for itself and the user key's own contract
// still carry.
func TestSwarmWalkCarriesNoUserAuthorityThroughAnUnlinkedContract(t *testing.T) {
	ctx := astral.NewContext(nil)
	w := newSwarmWorld(t)
	w.mod.Add(authmod.Func[*nodes.RelayForAction](func(_ *astral.Context, a *nodes.RelayForAction) bool {
		return a.Actor().IsEqual(a.ForID)
	}))
	relayFor := &auth.Permit{Action: astral.String8(nodes.RelayForAction{}.ObjectType())}
	adminSwarm := &auth.Permit{Action: astral.String8(user.AdminSwarmAction{}.ObjectType())}
	seedContract(t, w.mod, w.laptop, w.pi, relayFor, adminSwarm,
		&auth.Permit{Action: astral.String8(auth.SudoAction{}.ObjectType())},
		&auth.Permit{Action: astral.String8(user.ExpelAction{}.ObjectType())},
		permit(0))

	if w.mod.Authorize(ctx, &nodes.RelayForAction{Action: auth.NewAction(w.pi), ForID: w.user}) {
		t.Fatal("member relays as the user: want deny")
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

	seedContract(t, w.mod, w.appA, w.pi, relayFor)
	if !w.mod.Authorize(ctx, &nodes.RelayForAction{Action: auth.NewAction(w.pi), ForID: w.appA}) {
		t.Fatal("member relays for an app by the app's own contract: want allow")
	}

	seedContract(t, w.mod, w.user, w.member, adminSwarm)
	if !w.mod.Authorize(ctx, &user.AdminSwarmAction{Action: auth.NewAction(w.member)}) {
		t.Fatal("the user key's own contract: want allow")
	}
}
