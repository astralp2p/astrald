package fs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
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

// indexFile writes a payload to path and indexes path with the payload's full ID.
func indexFile(t *testing.T, repo *WatchRepository, path string, payload []byte) *astral.ObjectID {
	t.Helper()

	writeFile(t, path, payload)
	id := resolveID(t, payload)
	if err := repo.mod.db.IndexPath(path, id, 1); err != nil {
		t.Fatalf("IndexPath: %v", err)
	}

	return id
}

// TestHashTailEndsEveryDataIDOfTheHash: the query tail is a suffix of the data1 string of the hash at every size.
func TestHashTailEndsEveryDataIDOfTheHash(t *testing.T) {
	for _, hash := range [][32]byte{{}, {0, 0, 0, 5}, {0x80}, {0x7f, 0xff}, sha256.Sum256([]byte("hello"))} {
		for _, size := range []uint64{0, 1, 15, 16, 71, 1 << 40, math.MaxUint64} {
			id := astral.ObjectID{Size: size, Hash: hash}
			if tail := hashTail(hash); !strings.HasSuffix(id.String(), tail) {
				t.Errorf("hashTail(%x) = %v, not a suffix of %v", hash, tail, id.String())
			}
		}
	}

	if tail := hashTail(sha256.Sum256([]byte("hello"))); len(tail) != 51 {
		t.Errorf("len(hashTail) = %v, want 51 for a hash without leading zero bits after the first", len(tail))
	}
}

// TestWatchPartialLookup: a partial ID resolves through the index to an in-root file. Two paths of
// one full ID are one object, and an absent hash misses.
func TestWatchPartialLookup(t *testing.T) {
	root := t.TempDir()
	repo := testWatchRepository(t, root)
	ctx := astral.NewContext(nil)

	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	id := indexFile(t, repo, filepath.Join(root, "a.txt"), []byte("hello astral"))
	other := indexFile(t, repo, filepath.Join(root, "sub", "b.txt"), []byte("other"))
	indexFile(t, repo, filepath.Join(root, "sub", "copy.txt"), []byte("hello astral"))

	checkRead(t, repo, partialOf(t, id), []byte("hello astral"))
	checkRead(t, repo, partialOf(t, other), []byte("other"))
	if ok, err := repo.Contains(ctx, partialOf(t, id)); !ok || err != nil {
		t.Errorf("Contains(partial) = %v, %v, want true, nil", ok, err)
	}

	absent := partialOf(t, resolveID(t, []byte("absent")))
	if _, err := repo.Read(ctx, absent, 0, 0); !errors.Is(err, objects.ErrNotFound) {
		t.Errorf("Read(absent) = %v, want ErrNotFound", err)
	}
	if ok, err := repo.Contains(ctx, absent); ok || err != nil {
		t.Errorf("Contains(absent) = %v, %v, want false, nil", ok, err)
	}
}

// TestWatchPartialLookupReadsExistingRows: rows stored as data1 text before this change resolve with no
// re-index, including a Size 0 row whose data1 string lost hash characters to the leading 'y' strip.
func TestWatchPartialLookupReadsExistingRows(t *testing.T) {
	root := t.TempDir()
	repo := testWatchRepository(t, root)

	payload := []byte("hello astral")
	id := resolveID(t, payload)
	stripped := &astral.ObjectID{Hash: [32]byte{0, 0, 0, 5}}
	rows := map[string]*astral.ObjectID{"a.txt": id, "empty": stripped}

	for name, rowID := range rows {
		path := filepath.Join(root, name)
		writeFile(t, path, payload[:rowID.Size])
		err := repo.mod.db.Exec(
			"INSERT INTO fs__local_files (path, data_id, mod_time, updated_at) VALUES (?, ?, 1, 1)",
			path, rowID.String(),
		).Error
		if err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}

	for _, rowID := range rows {
		r, err := repo.Read(astral.NewContext(nil), partialOf(t, rowID), 0, 0)
		if err != nil {
			t.Fatalf("Read(partial %v): %v", rowID, err)
		}
		if got := r.ID(); !got.IsEqual(rowID) {
			t.Errorf("ID() = %v, want %v", got, rowID)
		}
		if data := readAndClose(t, r); !bytes.Equal(data, payload[:rowID.Size]) {
			t.Errorf("Read(partial %v) = %q, want %q", rowID, data, payload[:rowID.Size])
		}
	}
}

// TestWatchPartialLookupComparesTheWholeHash: two hashes that differ in the first bit alone share a
// query tail, and the lookup still returns the one asked for.
func TestWatchPartialLookupComparesTheWholeHash(t *testing.T) {
	root := t.TempDir()
	repo := testWatchRepository(t, root)

	asked := &astral.ObjectID{Size: 3, Hash: [32]byte{0x00, 1, 2, 3}}
	sibling := &astral.ObjectID{Size: 3, Hash: [32]byte{0x80, 1, 2, 3}}
	if hashTail(asked.Hash) != hashTail(sibling.Hash) {
		t.Fatalf("the two hashes do not share a query tail")
	}

	for name, rowID := range map[string]*astral.ObjectID{"asked": asked, "sibling": sibling} {
		path := filepath.Join(root, name)
		writeFile(t, path, []byte(name[:3]))
		if err := repo.mod.db.IndexPath(path, rowID, 1); err != nil {
			t.Fatalf("IndexPath: %v", err)
		}
	}

	r, err := repo.Read(astral.NewContext(nil), partialOf(t, asked), 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := r.ID(); !got.IsEqual(asked) {
		t.Errorf("ID() = %v, want %v", got, asked)
	}
	if data := readAndClose(t, r); string(data) != "ask" {
		t.Errorf("Read = %q, want %q", data, "ask")
	}
}

// TestWatchRootScoping: a row under a sibling root that shares the root's text prefix is outside the
// repository for reads and partial lookups. Full-ID Contains stays database-only and keeps matching it.
func TestWatchRootScoping(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "a")
	sibling := filepath.Join(base, "ab")
	for _, dir := range []string{root, sibling} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	repo := testWatchRepository(t, root)
	ctx := astral.NewContext(nil)

	id := indexFile(t, repo, filepath.Join(sibling, "f.txt"), []byte("hello astral"))

	for _, arg := range []*astral.ObjectID{id, partialOf(t, id)} {
		if _, err := repo.Read(ctx, arg, 0, 0); !errors.Is(err, objects.ErrNotFound) {
			t.Errorf("Read(%v) = %v, want ErrNotFound", arg, err)
		}
	}
	if ok, err := repo.Contains(ctx, partialOf(t, id)); ok || err != nil {
		t.Errorf("Contains(partial) = %v, %v, want false, nil", ok, err)
	}
	// note: this pins the unchanged full-ID path, whose prefix query also matches the sibling root.
	if ok, err := repo.Contains(ctx, id); !ok || err != nil {
		t.Errorf("Contains(full) = %v, %v, want true, nil", ok, err)
	}

	indexFile(t, repo, filepath.Join(root, "g.txt"), []byte("hello astral"))
	checkRead(t, repo, partialOf(t, id), []byte("hello astral"))
	checkRead(t, repo, id, []byte("hello astral"))
}

// TestNewWatchRepositoryCleansRoot: a configured root with a trailing separator is cleaned, so the rows
// the indexer stores under the cleaned root lie under the repository root.
func TestNewWatchRepositoryCleansRoot(t *testing.T) {
	root := t.TempDir()
	payload := []byte("hello astral")
	indexed := testWatchRepository(t, root)
	id := indexFile(t, indexed, filepath.Join(root, "a.txt"), payload)

	mod := indexed.mod
	mod.indexer = NewIndexer(mod)
	repo, err := NewWatchRepository(mod, root+string(filepath.Separator), "test")
	if err != nil {
		t.Fatalf("NewWatchRepository: %v", err)
	}
	t.Cleanup(func() { repo.watcher.Close() })

	if repo.root != root {
		t.Errorf("root = %v, want %v", repo.root, root)
	}
	checkRead(t, repo, id, payload)
	checkRead(t, repo, partialOf(t, id), payload)
}

// indexAll indexes a payload at each path in order and returns the payload's full ID.
func indexAll(t *testing.T, repo *WatchRepository, payload []byte, paths []string) *astral.ObjectID {
	t.Helper()

	var id *astral.ObjectID
	for _, path := range paths {
		id = indexFile(t, repo, path, payload)
	}

	return id
}

// TestWatchStaleRowUnderReplacedDirectory: a row whose parent directory became a regular file is a missing
// path, in either row order.
func TestWatchStaleRowUnderReplacedDirectory(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%v", reverse), func(t *testing.T) {
			root := t.TempDir()
			repo := testWatchRepository(t, root)
			ctx := astral.NewContext(nil)
			payload := []byte("hello astral")

			sub := filepath.Join(root, "sub")
			if err := os.Mkdir(sub, 0o700); err != nil {
				t.Fatal(err)
			}
			good := filepath.Join(root, "good.txt")
			paths := []string{filepath.Join(sub, "copy.txt"), good}
			if reverse {
				slices.Reverse(paths)
			}
			id := indexAll(t, repo, payload, paths)
			if err := os.RemoveAll(sub); err != nil {
				t.Fatal(err)
			}
			writeFile(t, sub, []byte("not a directory"))

			checkRead(t, repo, partialOf(t, id), payload)
			if ok, err := repo.Contains(ctx, partialOf(t, id)); !ok || err != nil {
				t.Errorf("Contains(partial) = %v, %v, want true, nil", ok, err)
			}

			if err := os.Remove(good); err != nil {
				t.Fatal(err)
			}
			if ok, err := repo.Contains(ctx, partialOf(t, id)); ok || err != nil {
				t.Errorf("Contains(partial) with only the stale row = %v, %v, want false, nil", ok, err)
			}
		})
	}
}

// TestWatchUnreadableRowDoesNotHideAnother: a row whose file cannot be checked does not fail a partial lookup
// that another row resolves, in either row order. With no other row, the error is returned.
func TestWatchUnreadableRowDoesNotHideAnother(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%v", reverse), func(t *testing.T) {
			root := t.TempDir()
			repo := testWatchRepository(t, root)
			ctx := astral.NewContext(nil)
			payload := []byte("hello astral")

			locked := filepath.Join(root, "locked")
			if err := os.Mkdir(locked, 0o700); err != nil {
				t.Fatal(err)
			}
			readable := filepath.Join(root, "b.txt")
			paths := []string{filepath.Join(locked, "a.txt"), readable}
			if reverse {
				slices.Reverse(paths)
			}
			id := indexAll(t, repo, payload, paths)
			if err := os.Chmod(locked, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(locked, 0o700) })
			if _, err := os.Stat(filepath.Join(locked, "a.txt")); !errors.Is(err, os.ErrPermission) {
				t.Skipf("a directory without permissions is still searchable: %v", err)
			}

			checkRead(t, repo, partialOf(t, id), payload)
			if ok, err := repo.Contains(ctx, partialOf(t, id)); !ok || err != nil {
				t.Errorf("Contains(partial) = %v, %v, want true, nil", ok, err)
			}

			if err := os.Remove(readable); err != nil {
				t.Fatal(err)
			}
			if ok, err := repo.Contains(ctx, partialOf(t, id)); ok || !errors.Is(err, os.ErrPermission) {
				t.Errorf("Contains(partial) with only the unreadable row = %v, %v, want false, a permission error", ok, err)
			}
			if _, err := repo.Read(ctx, partialOf(t, id), 0, 0); !errors.Is(err, os.ErrPermission) {
				t.Errorf("Read(partial) with only the unreadable row = %v, want a permission error", err)
			}
		})
	}
}

// TestWatchWrongLength: a file whose length no longer matches its row, or that is gone, is not the object.
// Full-ID Contains stays database-only.
func TestWatchWrongLength(t *testing.T) {
	root := t.TempDir()
	repo := testWatchRepository(t, root)
	ctx := astral.NewContext(nil)

	path := filepath.Join(root, "a.txt")
	id := indexFile(t, repo, path, []byte("hello astral"))
	writeFile(t, path, []byte("hello"))

	for _, arg := range []*astral.ObjectID{id, partialOf(t, id)} {
		if _, err := repo.Read(ctx, arg, 0, 0); !errors.Is(err, objects.ErrNotFound) {
			t.Errorf("Read(%v) = %v, want ErrNotFound", arg, err)
		}
	}
	if ok, err := repo.Contains(ctx, partialOf(t, id)); ok || err != nil {
		t.Errorf("Contains(partial) = %v, %v, want false, nil", ok, err)
	}
	if ok, err := repo.Contains(ctx, id); !ok || err != nil {
		t.Errorf("Contains(full) = %v, %v, want true, nil", ok, err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.Contains(ctx, partialOf(t, id)); ok || err != nil {
		t.Errorf("Contains(partial) of a removed file = %v, %v, want false, nil", ok, err)
	}
}

// TestWatchTwoSizesOfOneHashAreAmbiguous: two in-root files that each match their row's size and share
// a hash make a partial lookup ambiguous. Each full ID still reads its own file.
func TestWatchTwoSizesOfOneHashAreAmbiguous(t *testing.T) {
	root := t.TempDir()
	repo := testWatchRepository(t, root)
	ctx := astral.NewContext(nil)

	short := &astral.ObjectID{Size: 3, Hash: [32]byte{1, 2, 3}}
	long := &astral.ObjectID{Size: 5, Hash: short.Hash}
	for id, payload := range map[*astral.ObjectID]string{short: "abc", long: "abcde"} {
		path := filepath.Join(root, payload)
		writeFile(t, path, []byte(payload))
		if err := repo.mod.db.IndexPath(path, id, 1); err != nil {
			t.Fatalf("IndexPath: %v", err)
		}
	}
	partial := partialOf(t, short)

	if _, err := repo.Read(ctx, partial, 0, 0); !errors.Is(err, objects.ErrAmbiguousObjectID) {
		t.Errorf("Read = %v, want ErrAmbiguousObjectID", err)
	}
	if ok, err := repo.Contains(ctx, partial); ok || !errors.Is(err, objects.ErrAmbiguousObjectID) {
		t.Errorf("Contains = %v, %v, want false, ErrAmbiguousObjectID", ok, err)
	}

	for id, want := range map[*astral.ObjectID]string{short: "abc", long: "abcde"} {
		r, err := repo.Read(ctx, id, 0, 0)
		if err != nil {
			t.Fatalf("Read(%v): %v", id, err)
		}
		if data := readAndClose(t, r); string(data) != want {
			t.Errorf("Read(%v) = %q, want %q", id, data, want)
		}
	}
}

// TestWatchReadRanges: a full and a partial ID read the same windows.
func TestWatchReadRanges(t *testing.T) {
	root := t.TempDir()
	repo := testWatchRepository(t, root)

	checkReadRanges(t, repo, indexFile(t, repo, filepath.Join(root, "a.txt"), []byte("hello astral")))
}

// TestWatchCancelledLookup: the partial lookup query runs under the caller's context.
func TestWatchCancelledLookup(t *testing.T) {
	root := t.TempDir()
	repo := testWatchRepository(t, root)
	id := indexFile(t, repo, filepath.Join(root, "a.txt"), []byte("hello astral"))

	ctx, cancel := astral.NewContext(nil).WithCancel()
	cancel()

	if ok, err := repo.Contains(ctx, partialOf(t, id)); ok || !errors.Is(err, context.Canceled) {
		t.Errorf("Contains with a cancelled context = %v, %v, want false, context.Canceled", ok, err)
	}
}

// TestWatchDeleteIsUnsupported: the read-only repository refuses Delete for a full and a partial ID
// with errors.ErrUnsupported, which is not the hash lookup sentinel.
func TestWatchDeleteIsUnsupported(t *testing.T) {
	root := t.TempDir()
	repo := testWatchRepository(t, root)
	path := filepath.Join(root, "a.txt")
	id := indexFile(t, repo, path, []byte("hello astral"))

	for _, arg := range []*astral.ObjectID{id, partialOf(t, id)} {
		err := repo.Delete(astral.NewContext(nil), arg)
		if err != errors.ErrUnsupported || errors.Is(err, objects.ErrHashLookupUnsupported) {
			t.Errorf("Delete(%v) = %v, want errors.ErrUnsupported", arg, err)
		}
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("the file is gone after Delete: %v", err)
	}
}
