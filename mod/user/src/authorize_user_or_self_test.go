package user

import (
	"os"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
	authmod "github.com/astralp2p/astrald/mod/auth"
	authsrc "github.com/astralp2p/astrald/mod/auth/src"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	secp256k1sig "github.com/astralp2p/astrald/mod/secp256k1"
	"github.com/astralp2p/astrald/resources"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// walkCrypto verifies contract signatures with the secp256k1 ASN.1 scheme
// joinKeyring signs with. Any other method panics.
type walkCrypto struct{ cryptomod.Module }

func (walkCrypto) Verify(pub *crypto.PublicKey, sig *crypto.Signature, obj crypto.SignableTextObject) error {
	return secp256k1sig.VerifyASN1(pub, obj.SignableHash(), sig)
}

// walkAssets hands the auth loader an in-memory store and no config file.
type walkAssets struct{ db *gorm.DB }

var _ assets.Assets = walkAssets{}

func (walkAssets) Res() resources.Resources            { return nil }
func (walkAssets) Read(string) ([]byte, error)         { return nil, os.ErrNotExist }
func (walkAssets) Write(string, []byte) error          { return os.ErrPermission }
func (walkAssets) ReadData(string) ([]byte, error)     { return nil, os.ErrNotExist }
func (walkAssets) WriteData(string, []byte) error      { return os.ErrPermission }
func (walkAssets) LoadYAML(string, interface{}) error  { return resources.ErrNotFound }
func (walkAssets) StoreYAML(string, interface{}) error { return os.ErrPermission }
func (a walkAssets) Database() *gorm.DB                { return a.db }

// walkFixture is an unclaimed node running the real auth module, with this
// module's user-only handlers registered as deps.go registers them, and an app
// key the node can sign contracts with.
type walkFixture struct {
	mod       *Module
	authority authmod.Module
	keys      *joinKeyring
	node, app *astral.Identity
}

func newWalkFixture(t *testing.T) *walkFixture {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	keys := newJoinKeyring()
	f := &walkFixture{keys: keys, node: keys.newKey(), app: keys.newKey()}
	f.mod = &Module{node: &identityNode{id: f.node}}

	logger := log.New(f.node)
	logger.SetFilter(func(*log.Entry) bool { return false })
	loaded, err := authsrc.Loader{}.Load(f.mod.node, walkAssets{db: db}, logger)
	if err != nil {
		t.Fatalf("load auth: %v", err)
	}
	authority := loaded.(*authsrc.Module)
	authority.Crypto = walkCrypto{}
	f.authority = authority

	f.authority.Add(
		authmod.Func[*user.AdminSwarmAction](f.mod.AuthorizeAdminSwarm),
		authmod.Func[*user.ExpelAction](f.mod.AuthorizeExpel),
	)
	return f
}

// grant indexes a contract the issuer and the subject both sign, carrying one
// permit for each action type.
func (f *walkFixture) grant(t *testing.T, issuer, subject *astral.Identity, actions ...string) {
	t.Helper()

	c := &auth.Contract{
		Issuer:    issuer,
		Subject:   subject,
		ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
	}
	for _, a := range actions {
		c.Permits = append(c.Permits, &auth.Permit{Action: astral.String8(a)})
	}

	issuerSig, err := secp256k1sig.SignASN1(f.keys.key(issuer), c.SignableHash())
	if err != nil {
		t.Fatalf("issuer sign: %v", err)
	}
	subjectSig, err := secp256k1sig.SignASN1(f.keys.key(subject), c.SignableHash())
	if err != nil {
		t.Fatalf("subject sign: %v", err)
	}

	sc := &auth.SignedContract{Contract: c, IssuerSig: issuerSig, SubjectSig: subjectSig}
	if err := f.authority.IndexContract(astral.NewContext(nil), sc); err != nil {
		t.Fatalf("index contract: %v", err)
	}
}

// claim makes userID this node's user, as an active contract does.
func (f *walkFixture) claim(t *testing.T, userID *astral.Identity) {
	t.Helper()
	err := f.mod.config.ActiveContract.Set(nil, &auth.SignedContract{
		Contract: &auth.Contract{Issuer: userID, Subject: f.node},
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
}

// userOnlyActions are the user-only actions the fixture registers, each
// naming the given actor.
func userOnlyActions(actor *astral.Identity) []auth.ActionObject {
	return []auth.ActionObject{
		&user.AdminSwarmAction{Action: auth.NewAction(actor)},
		&user.ExpelAction{Action: auth.NewAction(actor), Subject: astral.GenerateIdentity()},
	}
}

// TestUnclaimedNodeGrantsAnAppAUserOnlyAction: an unclaimed node acts as the
// user, so it holds each user-only action itself and a contract it signs
// grants the action to an app through the auth walk. A stranger holds nothing.
func TestUnclaimedNodeGrantsAnAppAUserOnlyAction(t *testing.T) {
	ctx := astral.NewContext(nil)
	f := newWalkFixture(t)
	f.grant(t, f.node, f.app,
		user.AdminSwarmAction{}.ObjectType(), user.ExpelAction{}.ObjectType())

	for _, a := range userOnlyActions(f.node) {
		if !f.authority.Authorize(ctx, a) {
			t.Fatalf("%s: the unclaimed node acts as the user and must be allowed", a.ObjectType())
		}
	}
	for _, a := range userOnlyActions(f.app) {
		if !f.authority.Authorize(ctx, a) {
			t.Fatalf("%s: the app holds the unclaimed node's contract and must be allowed", a.ObjectType())
		}
	}
	for _, a := range userOnlyActions(astral.GenerateIdentity()) {
		if f.authority.Authorize(ctx, a) {
			t.Fatalf("%s: a stranger must be refused", a.ObjectType())
		}
	}
}

// TestClaimedNodeHoldsNoUserOnlyActionForItself: once a user claims the node,
// the node holds no user-only action for itself, so the contract it signed
// while unclaimed grants the app nothing, and the user holds each action.
func TestClaimedNodeHoldsNoUserOnlyActionForItself(t *testing.T) {
	ctx := astral.NewContext(nil)
	f := newWalkFixture(t)
	f.grant(t, f.node, f.app,
		user.AdminSwarmAction{}.ObjectType(), user.ExpelAction{}.ObjectType())

	userID := astral.GenerateIdentity()
	f.claim(t, userID)

	for _, a := range userOnlyActions(f.node) {
		if f.authority.Authorize(ctx, a) {
			t.Fatalf("%s: a claimed node must not hold a user-only action for itself", a.ObjectType())
		}
	}
	for _, a := range userOnlyActions(f.app) {
		if f.authority.Authorize(ctx, a) {
			t.Fatalf("%s: the claimed node's earlier contract must grant the app nothing", a.ObjectType())
		}
	}
	for _, a := range userOnlyActions(userID) {
		if !f.authority.Authorize(ctx, a) {
			t.Fatalf("%s: the user must be allowed", a.ObjectType())
		}
	}
}
