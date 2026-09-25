package fs

import (
	"bytes"
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// prefixTestDB returns an empty in-memory index for the prefix-range tests.
func prefixTestDB(t *testing.T) *DB {
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

	return db
}

// prefixTestID resolves a distinct object id from seed.
func prefixTestID(t *testing.T, seed string) *astral.ObjectID {
	t.Helper()

	id, err := astral.Resolve(bytes.NewReader([]byte(seed)))
	if err != nil {
		t.Fatalf("resolve %q: %v", seed, err)
	}
	return id
}

// prefixIndex indexes every path against id.
func prefixIndex(t *testing.T, db *DB, id *astral.ObjectID, paths ...string) {
	t.Helper()

	for _, p := range paths {
		if err := db.IndexPath(p, id, 1); err != nil {
			t.Fatalf("index %q: %v", p, err)
		}
	}
}

// prefixFoundPaths returns the paths FindObject reports, sorted.
func prefixFoundPaths(t *testing.T, db *DB, prefix string, id *astral.ObjectID) []string {
	t.Helper()

	rows, err := db.FindObject(prefix, id)
	if err != nil {
		t.Fatalf("FindObject(%q): %v", prefix, err)
	}

	var got []string
	for _, r := range rows {
		got = append(got, r.Path)
	}
	slices.Sort(got)
	return got
}

// TestDB_PrefixQueriesMatchOnlyStrictDescendants: a root of /data/music reaches
// neither the sibling directory /data/music2 nor the case variant /DATA/MUSIC,
// and never the root row itself.
func TestDB_PrefixQueriesMatchOnlyStrictDescendants(t *testing.T) {
	db := prefixTestDB(t)
	id := prefixTestID(t, "under")
	other := prefixTestID(t, "outside")

	prefixIndex(t, db, id, "/data/music/a.mp3", "/data/music/sub/b.mp3", "/data/music")
	prefixIndex(t, db, other, "/data/music2/c.mp3", "/DATA/MUSIC/d.mp3")

	want := []string{"/data/music/a.mp3", "/data/music/sub/b.mp3"}
	if got := prefixFoundPaths(t, db, "/data/music", id); !slices.Equal(got, want) {
		t.Errorf("FindObject: got %v; want %v", got, want)
	}

	ids, err := db.UniqueObjectIDs("/data/music")
	if err != nil {
		t.Fatalf("UniqueObjectIDs: %v", err)
	}
	if len(ids) != 1 || !ids[0].IsEqual(id) {
		t.Errorf("UniqueObjectIDs: got %v; want exactly the id stored under the root", ids)
	}

	exists, err := db.ObjectExists("/data/music", other)
	if err != nil {
		t.Fatalf("ObjectExists: %v", err)
	}
	if exists {
		t.Error("ObjectExists reported an object that lives only in a sibling directory and a case variant")
	}

	rows, err := db.FindByHashTail(t.Context(), "/data/music", other.String()[len(other.String())-4:])
	if err != nil {
		t.Fatalf("FindByHashTail: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("FindByHashTail reached outside the root: %v", rows)
	}
}

// TestDB_PrefixQueriesAcceptATrailingSeparator: deps.go hands NewWatchRepository
// the configured path uncleaned, so a root spelled with a trailing separator
// must behave as the same root.
func TestDB_PrefixQueriesAcceptATrailingSeparator(t *testing.T) {
	db := prefixTestDB(t)
	id := prefixTestID(t, "under")

	prefixIndex(t, db, id, "/data/music/a.mp3", "/data/music/sub/b.mp3")

	bare := prefixFoundPaths(t, db, "/data/music", id)
	trailing := prefixFoundPaths(t, db, "/data/music/", id)
	if !slices.Equal(bare, trailing) {
		t.Errorf("a trailing separator changed the result: %v vs %v", bare, trailing)
	}
	if len(bare) != 2 {
		t.Errorf("got %v; want both indexed paths", bare)
	}
}

// TestDB_PrefixQueriesTreatWildcardsLiterally: '_' and '%' are LIKE wildcards
// and '*', '?' and '[' are GLOB wildcards; a byte range holds none of them.
func TestDB_PrefixQueriesTreatWildcardsLiterally(t *testing.T) {
	db := prefixTestDB(t)
	id := prefixTestID(t, "under")

	prefixIndex(t, db, id, "/data/my_music/e.mp3", "/data/myXmusic/f.mp3")

	want := []string{"/data/my_music/e.mp3"}
	if got := prefixFoundPaths(t, db, "/data/my_music", id); !slices.Equal(got, want) {
		t.Errorf("'_' matched as a wildcard: got %v; want %v", got, want)
	}
}

// TestDB_EachPathExcludesSiblingsAndCaseVariants: EachPath carries the same
// predicate, and an empty prefix still visits everything.
func TestDB_EachPathExcludesSiblingsAndCaseVariants(t *testing.T) {
	db := prefixTestDB(t)
	id := prefixTestID(t, "under")

	all := []string{"/r", "/r/a", "/r/sub/b", "/rx/a", "/R/c"}
	prefixIndex(t, db, id, all...)

	collect := func(prefix string) []string {
		var got []string
		if err := db.EachPath(prefix, func(p string) error {
			got = append(got, p)
			return nil
		}); err != nil {
			t.Fatalf("EachPath(%q): %v", prefix, err)
		}
		slices.Sort(got)
		return got
	}

	want := []string{"/r/a", "/r/sub/b"}
	if got := collect("/r"); !slices.Equal(got, want) {
		t.Errorf("EachPath(%q): got %v; want %v", "/r", got, want)
	}

	wantAll := slices.Clone(all)
	slices.Sort(wantAll)
	if got := collect(""); !slices.Equal(got, wantAll) {
		t.Errorf("EachPath(%q): got %v; want %v", "", got, wantAll)
	}
}
