package archives

import (
	"bytes"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newTestModule returns a module over an empty in-memory index, with no node,
// no network and no dependencies wired.
func newTestModule(t *testing.T) *Module {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	// why: each connection to :memory: opens its own empty database.
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(&dbArchive{}, &dbEntry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &Module{db: db, log: log.New(nil)}
}

// grantingAuth grants SeeObjects for a named set of object ids and nothing else.
type grantingAuth struct {
	authmod.Module
	granted map[string]bool
}

func (a grantingAuth) Authorize(_ *astral.Context, action auth.ActionObject) bool {
	see, ok := action.(*auth.SeeObjectsAction)
	if !ok || see.ObjectID == nil {
		return false
	}
	return a.granted[see.ObjectID.String()]
}

// testObjectID resolves a distinct object id from seed.
func testObjectID(t *testing.T, seed string) *astral.ObjectID {
	t.Helper()

	id, err := astral.Resolve(bytes.NewReader([]byte(seed)))
	if err != nil {
		t.Fatalf("resolve %q: %v", seed, err)
	}
	return id
}

// indexEntry records entry as living inside the archive identified by archiveID.
func indexEntry(t *testing.T, mod *Module, archiveID, entryID *astral.ObjectID, path string) {
	t.Helper()

	archive := &dbArchive{ObjectID: archiveID, Format: "zip"}
	if err := mod.db.Create(archive).Error; err != nil {
		t.Fatalf("create archive: %v", err)
	}
	entry := &dbEntry{ParentID: archive.ID, Path: path, ObjectID: entryID}
	if err := mod.db.Create(entry).Error; err != nil {
		t.Fatalf("create entry: %v", err)
	}
}
