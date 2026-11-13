package vfs

import (
	"crypto/sha256"
	"testing"

	apiauth "github.com/astralp2p/astral-go/api/auth"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	authmod "github.com/astralp2p/astrald/mod/auth"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// testModule returns a module over an in-memory database with one connection, as the node has. A query on
// the module's database from inside a transaction would block here as it does on the node.
func testModule(t *testing.T) *Module {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	mod := &Module{
		config:   defaultConfig,
		identity: astral.GenerateIdentity(),
		log:      log.New(nil),
		db:       &DB{gdb},
	}
	if err := mod.db.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return mod
}

// createVFS creates a VFS and returns it.
func createVFS(t *testing.T, mod *Module, id *astral.Identity) *dbVolume {
	t.Helper()

	if err := mod.CreateVFS(id); err != nil {
		t.Fatalf("CreateVFS: %v", err)
	}

	vol, err := mod.db.findVolume(id)
	if err != nil {
		t.Fatalf("findVolume: %v", err)
	}

	return vol
}

// contentID returns the ObjectID of content.
func contentID(content string) astral.ObjectID {
	return astral.ObjectID{Size: uint64(len(content)), Hash: sha256.Sum256([]byte(content))}
}

const testModTime int64 = 1700000000000000000

// addFile inserts a file with the given content under a parent.
func addFile(t *testing.T, mod *Module, vol *dbVolume, parent int64, name, content string) *dbEntry {
	t.Helper()

	id := contentID(content)

	return insertEntry(t, mod, &dbEntry{VFS: vol.Identity, ParentID: parent, Name: name, ObjectID: &id, ModTime: testModTime})
}

// addFolder inserts a folder with a cleared ObjectID under a parent.
func addFolder(t *testing.T, mod *Module, vol *dbVolume, parent int64, name string) *dbEntry {
	t.Helper()

	return insertEntry(t, mod, &dbEntry{
		VFS: vol.Identity, ParentID: parent, Name: name, Flags: uint16(vfsapi.FlagFolder), ModTime: testModTime,
	})
}

func insertEntry(t *testing.T, mod *Module, row *dbEntry) *dbEntry {
	t.Helper()

	if err := mod.db.Create(row).Error; err != nil {
		t.Fatalf("insert %q: %v", row.Name, err)
	}

	return row
}

// fakeAuth answers Authorize from a set of permits: the action type, the actor and the VFS.
type fakeAuth struct {
	authmod.Module
	permits map[string]bool
	asked   []apiauth.ActionObject
}

func newFakeAuth() *fakeAuth { return &fakeAuth{permits: map[string]bool{}} }

func permitKey(actionType string, actor, vfs *astral.Identity) string {
	return actionType + "|" + actor.String() + "|" + vfs.String()
}

func (a *fakeAuth) permit(action astral.Object, actor, vfs *astral.Identity) {
	a.permits[permitKey(action.ObjectType(), actor, vfs)] = true
}

func (a *fakeAuth) Authorize(_ *astral.Context, action apiauth.ActionObject) bool {
	a.asked = append(a.asked, action)

	var vfs *astral.Identity
	switch v := action.(type) {
	case *vfsapi.ReadAction:
		vfs = v.VFSID
	case *vfsapi.WriteAction:
		vfs = v.VFSID
	}

	return a.permits[permitKey(action.ObjectType(), action.Actor(), vfs)]
}

// fakeUser answers the identity of the node's user.
type fakeUser struct {
	usermod.Module
	id *astral.Identity
}

func (u fakeUser) Identity() *astral.Identity { return u.id }

func testContext() *astral.Context { return astral.NewContext(nil) }
