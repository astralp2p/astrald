package fs

import (
	"errors"
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testLocalFilesDB(t *testing.T) *DB {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	db := &DB{DB: gdb}
	if err := db.AutoMigrate(&dbLocalFile{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func indexPath(t *testing.T, db *DB, path string, id *astral.ObjectID, modTime int64) {
	t.Helper()

	if err := db.IndexPath(path, id, modTime); err != nil {
		t.Fatalf("IndexPath(%q): %v", path, err)
	}
}

func eachPath(t *testing.T, db *DB, prefix string) []string {
	t.Helper()

	var paths []string
	err := db.EachPath(prefix, func(path string) error {
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatalf("EachPath(%q): %v", prefix, err)
	}
	slices.Sort(paths)
	return paths
}

func eachInvalidatedPath(t *testing.T, db *DB) []string {
	t.Helper()

	var paths []string
	err := db.EachInvalidatedPath(func(path string) error {
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatalf("EachInvalidatedPath: %v", err)
	}
	slices.Sort(paths)
	return paths
}

func objectExists(t *testing.T, db *DB, prefix string, id *astral.ObjectID) bool {
	t.Helper()

	exists, err := db.ObjectExists(prefix, id)
	if err != nil {
		t.Fatalf("ObjectExists(%q): %v", prefix, err)
	}
	return exists
}

func TestDB_IndexPath(t *testing.T) {
	db := testLocalFilesDB(t)
	id := resolveString(t, "a")

	indexPath(t, db, "/r/a", id, 1)
	indexPath(t, db, "/r/b", id, 2)

	if !objectExists(t, db, "/r", id) {
		t.Errorf("ObjectExists(\"/r\", id): got false, want true")
	}

	row, err := db.FindByPath("/r/a")
	if err != nil {
		t.Fatalf("FindByPath(\"/r/a\"): %v", err)
	}
	if row.ModTime != 1 {
		t.Errorf("FindByPath(\"/r/a\").ModTime: got %d, want 1", row.ModTime)
	}
	if row.DataID == nil || *row.DataID != *id {
		t.Errorf("FindByPath(\"/r/a\").DataID: got %v, want %v", row.DataID, id)
	}

	rows, err := db.FindObject("/r", id)
	if err != nil {
		t.Fatalf("FindObject: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("FindObject(\"/r\", id): got %d rows, want 2", len(rows))
	}

	ids, err := db.UniqueObjectIDs("/r")
	if err != nil {
		t.Fatalf("UniqueObjectIDs: %v", err)
	}
	if len(ids) != 1 || *ids[0] != *id {
		t.Errorf("UniqueObjectIDs(\"/r\"): got %v, want [%v]", ids, id)
	}
}

func TestDB_InvalidateAndValidatePaths(t *testing.T) {
	db := testLocalFilesDB(t)
	id := resolveString(t, "b")
	indexPath(t, db, "/r/b", id, 1)

	if err := db.InvalidatePaths([]string{"/r/b"}); err != nil {
		t.Fatalf("InvalidatePaths: %v", err)
	}

	if got := eachInvalidatedPath(t, db); !slices.Equal(got, []string{"/r/b"}) {
		t.Errorf("EachInvalidatedPath: got %v, want [/r/b]", got)
	}
	if objectExists(t, db, "/r", id) {
		t.Errorf("ObjectExists for an invalidated path: got true, want false")
	}

	if err := db.ValidatePaths([]string{"/r/b"}); err != nil {
		t.Fatalf("ValidatePaths: %v", err)
	}

	if got := eachInvalidatedPath(t, db); len(got) != 0 {
		t.Errorf("EachInvalidatedPath after ValidatePaths: got %v, want none", got)
	}
	if !objectExists(t, db, "/r", id) {
		t.Errorf("ObjectExists after ValidatePaths: got false, want true")
	}
}

func TestDB_SoftDeletePaths(t *testing.T) {
	db := testLocalFilesDB(t)
	id := resolveString(t, "a")
	indexPath(t, db, "/r/a", id, 1)

	if err := db.SoftDeletePaths([]string{"/r/a"}); err != nil {
		t.Fatalf("SoftDeletePaths: %v", err)
	}

	if objectExists(t, db, "/r", id) {
		t.Errorf("ObjectExists after SoftDeletePaths: got true, want false")
	}
	if got := eachPath(t, db, "/r"); len(got) != 0 {
		t.Errorf("EachPath(\"/r\") after SoftDeletePaths: got %v, want none", got)
	}

	if err := db.InvalidatePaths([]string{"/r/a"}); err != nil {
		t.Fatalf("InvalidatePaths: %v", err)
	}

	if got := eachPath(t, db, "/r"); !slices.Equal(got, []string{"/r/a"}) {
		t.Errorf("EachPath(\"/r\") after InvalidatePaths: got %v, want [/r/a]", got)
	}
	if _, err := db.FindByPath("/r/a"); err != nil {
		t.Errorf("FindByPath(\"/r/a\") after InvalidatePaths: got %v, want nil", err)
	}
}

func TestDB_EachPathPrefix(t *testing.T) {
	db := testLocalFilesDB(t)
	id := resolveString(t, "a")
	for _, path := range []string{"/r", "/r/a", "/r/sub/b", "/rx/a", "/gone"} {
		indexPath(t, db, path, id, 1)
	}
	if err := db.SoftDeletePaths([]string{"/gone"}); err != nil {
		t.Fatalf("SoftDeletePaths: %v", err)
	}

	if got, want := eachPath(t, db, "/r"), []string{"/r/a", "/r/sub/b"}; !slices.Equal(got, want) {
		t.Errorf("EachPath(\"/r\"): got %v, want %v", got, want)
	}
	if got, want := eachPath(t, db, ""), []string{"/r", "/r/a", "/r/sub/b", "/rx/a"}; !slices.Equal(got, want) {
		t.Errorf("EachPath(\"\"): got %v, want %v", got, want)
	}
}

func TestDB_LookupPaths(t *testing.T) {
	db := testLocalFilesDB(t)
	indexPath(t, db, "/r/a", resolveString(t, "a"), 1)

	empty, err := db.LookupPaths([]string{})
	if err != nil {
		t.Fatalf("LookupPaths([]): %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Errorf("LookupPaths([]): got %v (nil %v), want an empty non-nil map", empty, empty == nil)
	}

	found, err := db.LookupPaths([]string{"/r/a", "/nope"})
	if err != nil {
		t.Fatalf("LookupPaths: %v", err)
	}
	if len(found) != 1 || found["/r/a"] == nil {
		t.Errorf("LookupPaths([/r/a /nope]): got %v, want only /r/a", found)
	}
}

func TestDB_DeletePath(t *testing.T) {
	db := testLocalFilesDB(t)
	indexPath(t, db, "/r/a", resolveString(t, "a"), 1)

	if err := db.DeletePath("/r/a"); err != nil {
		t.Fatalf("DeletePath: %v", err)
	}

	if _, err := db.FindByPath("/r/a"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("FindByPath after DeletePath: got %v, want %v", err, gorm.ErrRecordNotFound)
	}
}
