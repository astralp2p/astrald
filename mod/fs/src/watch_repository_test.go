package fs

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// testWatchRepository returns a watch repository over root, backed by an in-memory index
// with no indexer and no file watcher.
func testWatchRepository(t *testing.T, root string) *WatchRepository {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	// why: each connection to :memory: opens its own empty database.
	sqlDB.SetMaxOpenConns(1)

	db := &DB{DB: gdb}
	if err := db.AutoMigrate(&dbLocalFile{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &WatchRepository{mod: &Module{db: db}, root: root}
}

// TestWatchReaderIDIsTheWholeObject: a reader over a window reports the indexed row's full ID,
// and neither the Read argument nor a returned ID aliases the reader's copy.
func TestWatchReaderIDIsTheWholeObject(t *testing.T) {
	root := t.TempDir()
	repo := testWatchRepository(t, root)
	payload := []byte("hello astral")

	id, err := astral.Resolve(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repo.mod.db.IndexPath(path, id, 1); err != nil {
		t.Fatalf("IndexPath: %v", err)
	}
	arg := *id

	r, err := repo.Read(astral.NewContext(nil), &arg, 2, 3)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "llo" {
		t.Fatalf("window = %q, want %q", data, "llo")
	}

	arg.Size++
	r.ID().Size++

	if got := r.ID(); !got.IsEqual(id) {
		t.Fatalf("ID() = %v, want %v", got, id)
	}
}
