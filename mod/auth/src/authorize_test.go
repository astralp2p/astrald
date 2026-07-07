package auth

import (
	"io"
	"testing"
	"time"

	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/mod/auth"
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
	mod.Add(auth.Func[*testAction](func(ctx *astral.Context, a *testAction) bool {
		return a.Actor().IsEqual(root)
	}))
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
