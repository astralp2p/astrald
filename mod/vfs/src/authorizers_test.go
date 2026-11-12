package vfs

import (
	"crypto/sha256"
	"io"
	"os"
	"testing"
	"time"

	apiauth "github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/core/assets"
	authmod "github.com/astralp2p/astrald/mod/auth"
	authsrc "github.com/astralp2p/astrald/mod/auth/src"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	"github.com/astralp2p/astrald/resources"
	"gorm.io/gorm"
)

// Fixtures for the real auth module: its chain walk and contract index decide, over the test module's store.

// authzVerifier accepts every signature. Signature checks are the auth module's concern; these tests cover
// which contracts are honoured once indexed.
//
// note: the embedded nil interface panics on any other method.
type authzVerifier struct{ cryptomod.Module }

func (authzVerifier) Verify(*crypto.PublicKey, *crypto.Signature, crypto.SignableTextObject) error {
	return nil
}

// authzAssets hands the auth module the test's store and nothing else.
type authzAssets struct{ db *gorm.DB }

var _ assets.Assets = authzAssets{}

func (authzAssets) Res() resources.Resources            { return nil }
func (authzAssets) Read(string) ([]byte, error)         { return nil, os.ErrNotExist }
func (authzAssets) Write(string, []byte) error          { return os.ErrPermission }
func (authzAssets) ReadData(string) ([]byte, error)     { return nil, os.ErrNotExist }
func (authzAssets) WriteData(string, []byte) error      { return os.ErrPermission }
func (authzAssets) LoadYAML(string, interface{}) error  { return resources.ErrNotFound }
func (authzAssets) StoreYAML(string, interface{}) error { return os.ErrPermission }
func (a authzAssets) Database() *gorm.DB                { return a.db }

// authzNode is the node the auth module is loaded on. It routes nothing.
type authzNode struct{ id *astral.Identity }

func (n authzNode) Identity() *astral.Identity { return n.id }

func (authzNode) RouteQuery(*astral.Context, *astral.InFlightQuery, io.WriteCloser) (io.WriteCloser, error) {
	return query.RouteNotFound()
}

// authzModule returns the vfs module with the real auth module over the same store and the module's handlers
// registered, on a node with a user.
func authzModule(t *testing.T) (*Module, *authsrc.Module) {
	t.Helper()

	mod := testModule(t)
	mod.User = fakeUser{id: astral.GenerateIdentity()}

	l := log.New(mod.identity)
	l.SetFilter(func(*log.Entry) bool { return false })

	loaded, err := authsrc.Loader{}.Load(authzNode{id: mod.identity}, authzAssets{db: mod.db.DB}, l)
	if err != nil {
		t.Fatalf("load auth: %v", err)
	}
	authority := loaded.(*authsrc.Module)
	authority.Crypto = authzVerifier{}

	mod.Auth = authority
	mod.addAuthorizers()

	return mod, authority
}

// authzContract is an unexpired contract from issuer to subject with one permit for action, constrained to the
// listed VFSes when any are given.
func authzContract(t *testing.T, issuer, subject *astral.Identity, action astral.Object, vfses ...*astral.Identity) *apiauth.SignedContract {
	t.Helper()

	permit := &apiauth.Permit{Action: astral.String8(action.ObjectType())}
	if len(vfses) > 0 {
		permit.Constraints = astral.NewBundle()
		for _, id := range vfses {
			if err := permit.Constraints.Append(id); err != nil {
				t.Fatalf("append constraint: %v", err)
			}
		}
	}

	sig := &crypto.Signature{Scheme: crypto.SchemeASN1, Data: astral.Bytes16("unchecked")}

	return &apiauth.SignedContract{
		Contract: &apiauth.Contract{
			Issuer:    issuer,
			Subject:   subject,
			Permits:   []*apiauth.Permit{permit},
			ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
		},
		IssuerSig:  sig,
		SubjectSig: sig,
	}
}

func authzIndex(t *testing.T, authority authmod.Module, sc *apiauth.SignedContract) {
	t.Helper()

	if err := authority.IndexContract(testContext(), sc); err != nil {
		t.Fatalf("index contract: %v", err)
	}
}

func readAction(actor, vfs *astral.Identity) *vfsapi.ReadAction {
	return &vfsapi.ReadAction{Action: apiauth.NewAction(actor), VFSID: vfs}
}

func writeAction(actor, vfs *astral.Identity) *vfsapi.WriteAction {
	return &vfsapi.WriteAction{Action: apiauth.NewAction(actor), VFSID: vfs}
}

func manageAction(actor *astral.Identity) *vfsapi.ManageAction {
	return &vfsapi.ManageAction{Action: apiauth.NewAction(actor)}
}

func seeAction(actor *astral.Identity, id *astral.ObjectID) *apiauth.SeeObjectsAction {
	return &apiauth.SeeObjectsAction{Action: apiauth.NewAction(actor), ObjectID: id}
}

func TestTheVFSIdentityIsAllowedOnItsOwnVFSOnly(t *testing.T) {
	mod := testModule(t)
	ctx := testContext()
	own, other := astral.GenerateIdentity(), astral.GenerateIdentity()

	if !mod.AuthorizeReadAction(ctx, readAction(own, own)) || !mod.AuthorizeWriteAction(ctx, writeAction(own, own)) {
		t.Fatal("the VFS identity is refused on its own VFS")
	}
	if mod.AuthorizeReadAction(ctx, readAction(own, other)) || mod.AuthorizeWriteAction(ctx, writeAction(own, other)) {
		t.Fatal("the VFS identity is allowed on another VFS")
	}
	if mod.AuthorizeManageAction(ctx, manageAction(own)) {
		t.Fatal("a VFS identity is allowed to manage")
	}
	if mod.AuthorizeReadAction(ctx, readAction(nil, nil)) || mod.AuthorizeWriteAction(ctx, writeAction(nil, nil)) {
		t.Fatal("the zero identity matches the zero VFS")
	}
}

func TestRootCallersAreAllowedEverything(t *testing.T) {
	mod := testModule(t)
	ctx := testContext()
	user, vfs := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod.User = fakeUser{id: user}

	for name, root := range map[string]*astral.Identity{"node": mod.identity, "user": user} {
		if !mod.AuthorizeReadAction(ctx, readAction(root, vfs)) ||
			!mod.AuthorizeWriteAction(ctx, writeAction(root, vfs)) ||
			!mod.AuthorizeManageAction(ctx, manageAction(root)) {
			t.Fatalf("the %v is refused", name)
		}
	}

	if mod.AuthorizeReadAction(ctx, readAction(user, nil)) {
		t.Fatal("a root caller is allowed on the zero VFS")
	}

	mod.User = fakeUser{}
	if mod.AuthorizeManageAction(ctx, manageAction(user)) {
		t.Fatal("a former user is allowed to manage on a node whose user identity is zero")
	}
}

func TestTheHandlersAnswerThroughTheAuthModule(t *testing.T) {
	mod, authority := authzModule(t)
	ctx := testContext()
	vfs, stranger := astral.GenerateIdentity(), astral.GenerateIdentity()

	switch {
	case !authority.Authorize(ctx, readAction(vfs, vfs)):
		t.Fatal("read of the own VFS is refused")
	case !authority.Authorize(ctx, writeAction(vfs, vfs)):
		t.Fatal("write of the own VFS is refused")
	case !authority.Authorize(ctx, manageAction(mod.identity)):
		t.Fatal("manage by the node is refused")
	case !authority.Authorize(ctx, manageAction(mod.User.Identity())):
		t.Fatal("manage by the user is refused")
	case authority.Authorize(ctx, readAction(stranger, vfs)):
		t.Fatal("read by a stranger is allowed")
	case authority.Authorize(ctx, manageAction(stranger)):
		t.Fatal("manage by a stranger is allowed")
	}
}

func TestAContractIsHonouredOnlyOnceIndexed(t *testing.T) {
	mod, authority := authzModule(t)
	ctx := testContext()
	vfs, app := astral.GenerateIdentity(), astral.GenerateIdentity()
	createVFS(t, mod, vfs)

	sc := authzContract(t, vfs, app, &vfsapi.ReadAction{}, vfs)

	if _, err := mod.authorizeVFS(ctx, app, vfs, accessRead); err != vfsapi.ErrNotPermitted {
		t.Fatalf("before indexing: %v; want not permitted", err)
	}

	authzIndex(t, authority, sc)

	if _, err := mod.authorizeVFS(ctx, app, vfs, accessRead); err != nil {
		t.Fatalf("after indexing: %v; want allowed", err)
	}
	if authority.Authorize(ctx, readAction(app, astral.GenerateIdentity())) {
		t.Fatal("the contract covers a VFS its constraints do not list")
	}
}

func TestAContractFromARootCallerIsHonoured(t *testing.T) {
	mod, authority := authzModule(t)
	ctx := testContext()
	vfs, app, operator := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()

	authzIndex(t, authority, authzContract(t, mod.User.Identity(), app, &vfsapi.WriteAction{}, vfs))
	authzIndex(t, authority, authzContract(t, mod.identity, operator, &vfsapi.ManageAction{}))

	if !authority.Authorize(ctx, writeAction(app, vfs)) {
		t.Fatal("a write contract from the user is not honoured")
	}
	if mod.authorizeManage(ctx, operator) != nil {
		t.Fatal("a manage contract from the node is not honoured")
	}
}

func TestAWritePermitGrantsNoRead(t *testing.T) {
	mod, authority := authzModule(t)
	ctx := testContext()
	vfs, app := astral.GenerateIdentity(), astral.GenerateIdentity()
	createVFS(t, mod, vfs)
	file := addFile(t, mod, mustVolume(t, mod, vfs), mustVolume(t, mod, vfs).RootID, "a.txt", "hello\n")

	authzIndex(t, authority, authzContract(t, vfs, app, &vfsapi.WriteAction{}, vfs))

	if _, err := mod.authorizeVFS(ctx, app, vfs, accessWrite); err != nil {
		t.Fatalf("write: %v; want allowed", err)
	}
	if _, err := mod.authorizeVFS(ctx, app, vfs, accessRead); err != vfsapi.ErrNotPermitted {
		t.Fatalf("read: %v; want not permitted", err)
	}
	if authority.Authorize(ctx, seeAction(app, file.ObjectID)) {
		t.Fatal("a write permit reads a file by ObjectID")
	}
}

func mustVolume(t *testing.T, mod *Module, id *astral.Identity) *dbVolume {
	t.Helper()

	vol, err := mod.db.findVolume(id)
	if err != nil {
		t.Fatalf("findVolume: %v", err)
	}

	return vol
}

// seeFixture is two VFSes, each with one file, a folder in the first with a cached ObjectID, and a fake auth.
type seeFixture struct {
	mod           *Module
	auth          *fakeAuth
	vfs, other    *astral.Identity
	file, foreign *astral.ObjectID
	folder        *astral.ObjectID
}

func newSeeFixture(t *testing.T) *seeFixture {
	t.Helper()

	f := &seeFixture{mod: testModule(t), auth: newFakeAuth(), vfs: astral.GenerateIdentity(), other: astral.GenerateIdentity()}
	f.mod.Auth = f.auth

	vol, otherVol := createVFS(t, f.mod, f.vfs), createVFS(t, f.mod, f.other)
	f.file = addFile(t, f.mod, vol, vol.RootID, "mine", "mine").ObjectID
	f.foreign = addFile(t, f.mod, otherVol, otherVol.RootID, "theirs", "theirs").ObjectID

	folderID := contentID("a listing")
	f.folder = &folderID
	insertEntry(t, f.mod, &dbEntry{
		VFS: f.vfs, ParentID: vol.RootID, Name: "dir", Flags: uint16(vfsapi.FlagFolder), ObjectID: f.folder,
		ModTime: testModTime,
	})

	return f
}

func (f *seeFixture) see(actor *astral.Identity, id *astral.ObjectID) bool {
	return f.mod.AuthorizeSeeObjects(testContext(), seeAction(actor, id))
}

func TestSeeObjectsFollowsTheVFSOfTheFile(t *testing.T) {
	f := newSeeFixture(t)

	switch {
	case !f.see(f.vfs, f.file):
		t.Fatal("the VFS identity is refused its own file")
	case f.see(f.vfs, f.foreign):
		t.Fatal("the VFS identity reads a file of another VFS")
	case f.see(f.vfs, &astral.ObjectID{Size: 4, Hash: sha256.Sum256([]byte("none"))}):
		t.Fatal("an unknown ObjectID is allowed")
	case f.see(f.vfs, f.folder):
		t.Fatal("a folder's cached ObjectID is allowed")
	case f.see(f.vfs, nil):
		t.Fatal("an action without ObjectID is allowed")
	case f.see(nil, f.file):
		t.Fatal("the zero identity is allowed")
	}
}

func TestSeeObjectsAllowsAReadPermitAndNotAWritePermit(t *testing.T) {
	f := newSeeFixture(t)
	reader, writer := astral.GenerateIdentity(), astral.GenerateIdentity()
	f.auth.permit(&vfsapi.ReadAction{}, reader, f.vfs)
	f.auth.permit(&vfsapi.WriteAction{}, writer, f.vfs)

	if !f.see(reader, f.file) {
		t.Fatal("a read permit holder is refused")
	}
	if f.see(reader, f.foreign) {
		t.Fatal("a read permit holder reads a file of a VFS the permit does not name")
	}
	if f.see(writer, f.file) {
		t.Fatal("a write permit holder is allowed")
	}
}

func TestSeeObjectsIgnoresTheRepo(t *testing.T) {
	f := newSeeFixture(t)
	action := seeAction(f.vfs, f.file)
	action.Repo = "anything"

	if !f.mod.AuthorizeSeeObjects(testContext(), action) {
		t.Fatal("a named repository changes the answer")
	}
}

func TestSeeObjectsRefusesAPartialObjectID(t *testing.T) {
	f := newSeeFixture(t)
	vol := mustVolume(t, f.mod, f.vfs)

	partial := &astral.ObjectID{Hash: f.file.Hash}
	insertEntry(t, f.mod, &dbEntry{VFS: f.vfs, ParentID: vol.RootID, Name: "partial", ObjectID: partial, ModTime: testModTime})

	if f.see(f.vfs, partial) {
		t.Fatal("an ObjectID of size 0 is allowed")
	}
}

func TestSeeObjectsAllowsTheEmptyObject(t *testing.T) {
	f := newSeeFixture(t)
	vol := mustVolume(t, f.mod, f.vfs)
	empty := contentID("")

	if f.see(f.vfs, &empty) {
		t.Fatal("the empty object is allowed with no empty file")
	}

	addFile(t, f.mod, vol, vol.RootID, "empty", "")

	if !f.see(f.vfs, &empty) {
		t.Fatal("the empty object is refused for the VFS that has an empty file")
	}
	if f.see(f.other, &empty) {
		t.Fatal("the empty object is allowed for a VFS without an empty file")
	}
}

func TestSeeObjectsRefusesOnADatabaseError(t *testing.T) {
	f := newSeeFixture(t)

	sqlDB, err := f.mod.db.DB.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if f.see(f.vfs, f.file) {
		t.Fatal("a database error allows")
	}
}

// TestSeeObjectsIsNodeLocal checks that the handler answers for the caller alone: a VFS identity cannot hand
// reading its files by ObjectID down a contract chain.
func TestSeeObjectsIsNodeLocal(t *testing.T) {
	mod, authority := authzModule(t)
	ctx := testContext()
	vfs, app := astral.GenerateIdentity(), astral.GenerateIdentity()
	vol := createVFS(t, mod, vfs)
	file := addFile(t, mod, vol, vol.RootID, "a.txt", "hello\n")

	authzIndex(t, authority, authzContract(t, vfs, app, &apiauth.SeeObjectsAction{}))

	if !authority.Authorize(ctx, seeAction(vfs, file.ObjectID)) {
		t.Fatal("the VFS identity is refused its own file")
	}
	if authority.Authorize(ctx, seeAction(app, file.ObjectID)) {
		t.Fatal("a see_objects contract from the VFS identity reads its file")
	}

	authzIndex(t, authority, authzContract(t, vfs, app, &vfsapi.ReadAction{}, vfs))

	if !authority.Authorize(ctx, seeAction(app, file.ObjectID)) {
		t.Fatal("a read contract from the VFS identity does not read its file by ObjectID")
	}
}

// TestDescribeAndFindDoNotReachTheHandler checks the action objects.describe and objects.find ask: it names no
// ObjectID, and the handler refuses an action without one. mod/objects tests that the two ops ask it so.
func TestDescribeAndFindDoNotReachTheHandler(t *testing.T) {
	f := newSeeFixture(t)

	if f.see(f.vfs, nil) {
		t.Fatal("an action without ObjectID is allowed")
	}
}

func TestCheckManageOriginRefusesTheNetwork(t *testing.T) {
	id := astral.GenerateIdentity()
	q := query.New(id, id, "vfs.volumes", nil)

	if err := checkManageOrigin(routing.NewIncomingQuery(q, nil, astral.OriginNetwork)); err == nil {
		t.Fatal("a query from the network is accepted")
	}
	if err := checkManageOrigin(routing.NewIncomingQuery(q, nil, "")); err != nil {
		t.Fatalf("a local query is refused: %v", err)
	}
}
