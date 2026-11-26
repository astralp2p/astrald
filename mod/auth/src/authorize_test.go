package auth

import (
	authmod "github.com/astralp2p/astrald/mod/auth"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// testAction mirrors real actions: constraints pass only when absent.
type testAction struct {
	auth.Action
}

// astral:blueprint-ignore
func (testAction) ObjectType() string { return "test.action" }

func (a testAction) WriteTo(w io.Writer) (int64, error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *testAction) ReadFrom(r io.Reader) (int64, error) {
	return astral.Objectify(a).ReadFrom(r)
}

func (a testAction) ApplyConstraints(cs *astral.Bundle) bool {
	return cs == nil || len(cs.Objects()) == 0
}

func testModule(t *testing.T) *Module {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	db := &DB{gdb}
	if err := db.AutoMigrate(&dbContract{}, &dbContractPermit{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &Module{db: db, log: log.New(astral.GenerateIdentity())}
}

// allowRoot registers a direct rule allowing only root to perform testAction.
func allowRoot(mod *Module, root *astral.Identity) {
	mod.Add(authmod.Func[*testAction](func(ctx *astral.Context, a *testAction) bool {
		return a.Actor().IsEqual(root)
	}))
}

// nodeLocalRoot registers the same rule as allowRoot on the node-local rail:
// it stands in for a node-local handler such as sys's or vfs's, which answers
// from a record this node keeps and no other node reads.
func nodeLocalRoot(mod *Module, root *astral.Identity) {
	mod.Add(authmod.NodeLocal(authmod.Func[*testAction](func(ctx *astral.Context, a *testAction) bool {
		return a.Actor().IsEqual(root)
	})))
}

func dummySig() *crypto.Signature {
	return &crypto.Signature{Scheme: crypto.SchemeASN1, Data: astral.Bytes16("dummy")}
}

// seedContract stores an issuer→subject contract carrying the given permits.
func seedContract(t *testing.T, mod *Module, issuer, subject *astral.Identity, permits ...*auth.Permit) {
	t.Helper()

	sc := &auth.SignedContract{
		Contract: &auth.Contract{
			Issuer:    issuer,
			Subject:   subject,
			Permits:   permits,
			ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
		},
		IssuerSig:  dummySig(),
		SubjectSig: dummySig(),
	}

	if err := mod.db.storeSignedContract(sc); err != nil {
		t.Fatalf("store contract: %v", err)
	}
}

func permit(delegation uint8) *auth.Permit {
	return &auth.Permit{Action: "test.action", Delegation: astral.Uint8(delegation)}
}

func constrainedPermit(delegation uint8) *auth.Permit {
	cs := astral.NewBundle()
	if err := cs.Append(&astral.Ack{}); err != nil {
		panic(err)
	}
	return &auth.Permit{Action: "test.action", Constraints: cs, Delegation: astral.Uint8(delegation)}
}

func action(actor *astral.Identity) *testAction {
	return &testAction{Action: auth.NewAction(actor)}
}

func TestAuthorizeDirect(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	other := astral.GenerateIdentity()
	allowRoot(mod, root)

	if !mod.Authorize(ctx, action(root)) {
		t.Fatal("root: want allow")
	}
	if mod.Authorize(ctx, action(other)) {
		t.Fatal("other: want deny")
	}
}

func TestAuthorizeOneHop(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	leaf := astral.GenerateIdentity()
	allowRoot(mod, root)

	// a non-delegable permit still authorizes the direct subject
	seedContract(t, mod, root, leaf, permit(0))

	if !mod.Authorize(ctx, action(leaf)) {
		t.Fatal("leaf: want allow via one-hop contract")
	}
}

func TestAuthorizeTwoHopChain(t *testing.T) {
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	mid := astral.GenerateIdentity()
	leaf := astral.GenerateIdentity()

	// root link permits one hop below it: chain resolves
	mod := testModule(t)
	allowRoot(mod, root)
	seedContract(t, mod, root, mid, permit(1))
	seedContract(t, mod, mid, leaf, permit(0))
	if !mod.Authorize(ctx, action(leaf)) {
		t.Fatal("leaf: want allow via two-hop chain")
	}
	if !mod.Authorize(ctx, action(mid)) {
		t.Fatal("mid: want allow via one-hop chain")
	}

	// root link non-delegable: mid still passes, leaf does not
	mod = testModule(t)
	allowRoot(mod, root)
	seedContract(t, mod, root, mid, permit(0))
	seedContract(t, mod, mid, leaf, permit(0))
	if mod.Authorize(ctx, action(leaf)) {
		t.Fatal("leaf: want deny - root link is non-delegable")
	}
	if !mod.Authorize(ctx, action(mid)) {
		t.Fatal("mid: want allow - direct subject of the root link")
	}
}

func TestAuthorizeInflatedMiddle(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	mid := astral.GenerateIdentity()
	leaf := astral.GenerateIdentity()
	allowRoot(mod, root)

	// the middle link overpromises; the root link's 0 caps the chain
	seedContract(t, mod, root, mid, permit(0))
	seedContract(t, mod, mid, leaf, permit(255))

	if mod.Authorize(ctx, action(leaf)) {
		t.Fatal("leaf: want deny - intermediary cannot inflate delegation")
	}
}

func TestAuthorizeConstraintOnRootLink(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	mid := astral.GenerateIdentity()
	leaf := astral.GenerateIdentity()
	allowRoot(mod, root)

	// constraints apply at every link: the root link's constraints fail the
	// action even though the leaf link is unconstrained
	seedContract(t, mod, root, mid, constrainedPermit(1))
	seedContract(t, mod, mid, leaf, permit(0))

	if mod.Authorize(ctx, action(leaf)) {
		t.Fatal("leaf: want deny - root link constraints must apply")
	}
}

func TestAuthorizeCycle(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	a := astral.GenerateIdentity()
	b := astral.GenerateIdentity()
	allowRoot(mod, astral.GenerateIdentity()) // root unreachable from the cycle

	seedContract(t, mod, a, b, permit(255))
	seedContract(t, mod, b, a, permit(255))

	if mod.Authorize(ctx, action(a)) {
		t.Fatal("a: want deny - cyclic contracts grant nothing")
	}
}

// TestNodeLocalIsNotSatisfiableByAContract is the first bar: an action authorized
// by a node-local record is not reachable through a signed contract, at any
// chain length and under the widest permits a chain can carry. Only the holder
// of the record is authorized.
func TestNodeLocalIsNotSatisfiableByAContract(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	mid := astral.GenerateIdentity()
	leaf := astral.GenerateIdentity()
	stranger := astral.GenerateIdentity()
	nodeLocalRoot(mod, root)

	seedContract(t, mod, root, mid, permit(255))
	seedContract(t, mod, mid, leaf, permit(255))
	// a contract from an identity no node-local record names
	seedContract(t, mod, stranger, leaf, permit(255))

	if mod.Authorize(ctx, action(mid)) {
		t.Fatal("mid: want deny - a contract does not reach the node-local rail")
	}
	if mod.Authorize(ctx, action(leaf)) {
		t.Fatal("leaf: want deny - no chain length reaches the node-local rail")
	}
	if !mod.Authorize(ctx, action(root)) {
		t.Fatal("root: want allow - the node-local record still decides for its holder")
	}
}

// TestNodeLocalIsNotExtendedAHopByItsHolder is the second bar: the holder of a
// node-local record issues a contract for the same action, and the walk must
// not re-enter the node-local rail as the issuer.
func TestNodeLocalIsNotExtendedAHopByItsHolder(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	leaf := astral.GenerateIdentity()
	nodeLocalRoot(mod, root)

	// the widest contract the holder could write, delegation included
	seedContract(t, mod, root, leaf, permit(255))

	if mod.Authorize(ctx, action(leaf)) {
		t.Fatal("leaf: want deny - a node-local holder cannot extend its record a hop")
	}
	if !mod.Authorize(ctx, action(root)) {
		t.Fatal("root: want allow - issuing a contract does not cost the holder its record")
	}
}

// TestContractRailIsUnchangedByTheNodeLocalRail is the no-regression bar: a handler
// that is not node-local still roots a chain, which is what a node→app contract
// rests on.
func TestContractRailIsUnchangedByTheNodeLocalRail(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	mid := astral.GenerateIdentity()
	leaf := astral.GenerateIdentity()
	allowRoot(mod, root)
	seedContract(t, mod, root, mid, permit(1))
	seedContract(t, mod, mid, leaf, permit(0))

	if !mod.Authorize(ctx, action(leaf)) {
		t.Fatal("leaf: want allow - a portable authority still delegates")
	}
}

func TestAuthorizeActorRestored(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	mid := astral.GenerateIdentity()
	leaf := astral.GenerateIdentity()
	allowRoot(mod, root)
	seedContract(t, mod, root, mid, permit(1))
	seedContract(t, mod, mid, leaf, permit(0))

	allowed := action(leaf)
	if !mod.Authorize(ctx, allowed) {
		t.Fatal("leaf: want allow")
	}
	if !allowed.Actor().IsEqual(leaf) {
		t.Fatal("actor not restored after an allowed walk")
	}

	denied := action(astral.GenerateIdentity())
	if mod.Authorize(ctx, denied) {
		t.Fatal("stranger: want deny")
	}
	if denied.Actor().IsEqual(leaf) || denied.Actor().IsEqual(root) {
		t.Fatal("actor not restored after a denied walk")
	}
}

func sudoAction(actor, as *astral.Identity) *auth.SudoAction {
	return &auth.SudoAction{Action: auth.NewAction(actor), AsID: as}
}

// TestAuthorizeRefusesAnEmptyActor covers an apphost session without a token,
// which names no actor. A handler that allows everyone, a sudo contract made out
// to another identity, and an authority that allows everyone each grant nothing.
func TestAuthorizeRefusesAnEmptyActor(t *testing.T) {
	mod := testModule(t)
	mod.Add(authmod.Func[*auth.SudoAction](mod.AuthorizeSudo))
	mod.Add(authmod.Func[*testAction](func(*astral.Context, *testAction) bool { return true }))
	stub := withAuthority(t, mod)
	stub.answer = true
	ctx := astral.NewContext(nil)

	user := astral.GenerateIdentity()
	wallet := astral.GenerateIdentity()
	seedContract(t, mod, user, wallet, &auth.Permit{Action: astral.String8(auth.SudoAction{}.ObjectType())})

	for _, empty := range []*astral.Identity{nil, {}} {
		if mod.Authorize(ctx, action(empty)) {
			t.Fatal("allow-all handler: want deny for an empty actor")
		}
		if mod.Authorize(ctx, sudoAction(empty, user)) {
			t.Fatal("sudo contract for another subject: want deny for an empty actor")
		}
	}
	if len(stub.asked) > 0 {
		t.Fatal("the authority was asked about an empty actor")
	}

	if !mod.Authorize(ctx, action(wallet)) {
		t.Fatal("allow-all handler: want allow for a named actor")
	}
	if !mod.Authorize(ctx, sudoAction(wallet, user)) {
		t.Fatal("sudo contract: want allow for its subject")
	}
}

// TestAuthorizeSudoAllowsAnIdentityToBeItself pins the rule the zero-target
// refusal must not widen.
func TestAuthorizeSudoAllowsAnIdentityToBeItself(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	id := astral.GenerateIdentity()

	if !mod.AuthorizeSudo(ctx, sudoAction(id, id)) {
		t.Fatal("self: want allow")
	}
	if mod.AuthorizeSudo(ctx, sudoAction(id, astral.GenerateIdentity())) {
		t.Fatal("other: want deny")
	}
}

// TestAuthorizeSudoRefusesTheZeroTarget covers the identity Dir.ResolveIdentity
// returns for the empty name and "anyone". The second case is the one the self
// test alone misses: Identity.IsEqual answers true when both sides are zero.
func TestAuthorizeSudoRefusesTheZeroTarget(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	zero := &astral.Identity{}

	if mod.AuthorizeSudo(ctx, sudoAction(astral.GenerateIdentity(), zero)) {
		t.Fatal("named actor, zero target: want deny")
	}
	if mod.AuthorizeSudo(ctx, sudoAction(zero, zero)) {
		t.Fatal("zero actor, zero target: want deny")
	}
}

// vetoing registers a veto that refuses the named identities and returns the
// set, so a test can lift a refusal and count none.
func vetoing(mod *Module, ids ...*astral.Identity) map[string]bool {
	set := map[string]bool{}
	for _, id := range ids {
		set[id.String()] = true
	}
	mod.AddVeto(func(actor *astral.Identity) bool { return set[actor.String()] })
	return set
}

// TestAuthorizeVetoRefusesTheActor: a vetoed actor holds a handler's allow and a
// contract's permit alike and gets neither; an identity no veto names is
// unaffected.
func TestAuthorizeVetoRefusesTheActor(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	member := astral.GenerateIdentity()
	other := astral.GenerateIdentity()
	allowRoot(mod, root)
	seedContract(t, mod, root, member, permit(0))
	seedContract(t, mod, root, other, permit(0))
	vetoing(mod, member)

	if mod.Authorize(ctx, action(member)) {
		t.Fatal("member: want deny - the contract permits it but the veto names it")
	}
	if !mod.Authorize(ctx, action(other)) {
		t.Fatal("other: want allow - no veto names it")
	}
	if !mod.Authorize(ctx, action(root)) {
		t.Fatal("root: want allow - no veto names it")
	}

	// a handler's allow is no more a defence than a contract's
	mod = testModule(t)
	allowRoot(mod, root)
	vetoing(mod, root)
	if mod.Authorize(ctx, action(root)) {
		t.Fatal("root: want deny - a handler allows it but the veto names it")
	}
}

// TestAuthorizeVetoRefusesAManagementSubject is the expelled management node:
// the subject of a delegable contract from an allowed root is refused once
// vetoed, while a second subject of the same contract shape still passes.
func TestAuthorizeVetoRefusesAManagementSubject(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	expelled := astral.GenerateIdentity()
	sibling := astral.GenerateIdentity()
	allowRoot(mod, root)
	seedContract(t, mod, root, expelled, permit(1))
	seedContract(t, mod, root, sibling, permit(1))

	if !mod.Authorize(ctx, action(expelled)) {
		t.Fatal("expelled: want allow before the veto")
	}
	vetoing(mod, expelled)

	if mod.Authorize(ctx, action(expelled)) {
		t.Fatal("expelled: want deny - its contract still permits the action")
	}
	if !mod.Authorize(ctx, action(sibling)) {
		t.Fatal("sibling: want allow - same contract shape, not vetoed")
	}
}

// TestAuthorizeVetoRefusesAChainThroughAVetoedIssuer: a leaf whose only path
// runs through a vetoed issuer is refused, one with an independent path is not,
// and the vetoed issuer's own walk does not end the leaf's other contracts.
func TestAuthorizeVetoRefusesAChainThroughAVetoedIssuer(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	vetoed := astral.GenerateIdentity()
	clean := astral.GenerateIdentity()
	onlyVia := astral.GenerateIdentity()
	both := astral.GenerateIdentity()
	allowRoot(mod, root)
	seedContract(t, mod, root, vetoed, permit(1))
	seedContract(t, mod, root, clean, permit(1))
	seedContract(t, mod, vetoed, onlyVia, permit(0))
	seedContract(t, mod, vetoed, both, permit(0))
	seedContract(t, mod, clean, both, permit(0))

	for _, id := range []*astral.Identity{onlyVia, both} {
		if !mod.Authorize(ctx, action(id)) {
			t.Fatalf("%v: want allow before the veto", id)
		}
	}
	vetoing(mod, vetoed)

	if mod.Authorize(ctx, action(onlyVia)) {
		t.Fatal("onlyVia: want deny - its issuer is vetoed")
	}
	if !mod.Authorize(ctx, action(both)) {
		t.Fatal("both: want allow - the path through the clean issuer stands")
	}
	if !mod.Authorize(ctx, action(clean)) {
		t.Fatal("clean: want allow")
	}
}

// TestAuthorizeVetoLiftsWithTheSet: the veto is asked at each authorization, so
// an identity it stops naming is allowed again, and the actor of a refused walk
// is left as it was.
func TestAuthorizeVetoLiftsWithTheSet(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	mid := astral.GenerateIdentity()
	leaf := astral.GenerateIdentity()
	allowRoot(mod, root)
	seedContract(t, mod, root, mid, permit(1))
	seedContract(t, mod, mid, leaf, permit(0))
	set := vetoing(mod, mid)

	refused := action(leaf)
	if mod.Authorize(ctx, refused) {
		t.Fatal("leaf: want deny while mid is vetoed")
	}
	if !refused.Actor().IsEqual(leaf) {
		t.Fatal("actor not restored after a vetoed walk")
	}

	delete(set, mid.String())
	if !mod.Authorize(ctx, action(leaf)) {
		t.Fatal("leaf: want allow once the veto lifts")
	}
}

// TestAuthorizeVetoRefusesSudo: a vetoed actor gets no sudo, though it holds a
// contract naming the action, and the identity it would act as is not asked.
func TestAuthorizeVetoRefusesSudo(t *testing.T) {
	mod := testModule(t)
	mod.Add(authmod.Func[*auth.SudoAction](mod.AuthorizeSudo))
	ctx := astral.NewContext(nil)
	user := astral.GenerateIdentity()
	wallet := astral.GenerateIdentity()
	seedContract(t, mod, user, wallet, &auth.Permit{Action: astral.String8(auth.SudoAction{}.ObjectType())})

	if !mod.Authorize(ctx, sudoAction(wallet, user)) {
		t.Fatal("wallet: want sudo before the veto")
	}
	vetoing(mod, wallet)

	if mod.Authorize(ctx, sudoAction(wallet, user)) {
		t.Fatal("wallet: want deny - the veto names it")
	}
	if !mod.Authorize(ctx, sudoAction(user, user)) {
		t.Fatal("user: want sudo as itself - not vetoed")
	}
}

// TestAuthorizeVetoPrecedesTheExternalAuthority: no question about a vetoed
// actor reaches the authority, at the first level or at an issuer's.
func TestAuthorizeVetoPrecedesTheExternalAuthority(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	vetoedActor := astral.GenerateIdentity()
	issuer := astral.GenerateIdentity()
	subject := astral.GenerateIdentity()
	seedContract(t, mod, issuer, subject, permit(0))

	// the authority permits the vetoed actor and the issuer, and not the subject
	stub := withAuthority(t, mod)
	stub.allow = map[string]bool{vetoedActor.String(): true, issuer.String(): true}

	if !mod.Authorize(ctx, action(vetoedActor)) || !mod.Authorize(ctx, action(subject)) {
		t.Fatal("want allow from the authority, directly and through the issuer, before the veto")
	}
	stub.asked = nil
	vetoing(mod, vetoedActor, issuer)

	if mod.Authorize(ctx, action(vetoedActor)) {
		t.Fatal("vetoed actor: want deny though the authority allows it")
	}
	if mod.Authorize(ctx, action(subject)) {
		t.Fatal("subject: want deny - the only path runs through a vetoed issuer")
	}
	if stub.askedAbout(vetoedActor) || stub.askedAbout(issuer) {
		t.Fatalf("the authority was asked about a vetoed identity: %v", stub.asked)
	}
	// note: the subject's own level is not vetoed, so the authority is asked there.
	if !stub.askedAbout(subject) {
		t.Fatal("the authority was not asked about the unvetoed subject")
	}
}
