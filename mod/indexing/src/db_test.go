package indexing

import (
	"bytes"
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/indexing"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testChangelogDB(t *testing.T) *DB {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	db, err := newDB(gdb)
	if err != nil {
		t.Fatalf("newDB: %v", err)
	}
	if err := db.autoMigrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func changelogID(t *testing.T, data string) *astral.ObjectID {
	t.Helper()

	id, err := astral.Resolve(bytes.NewReader([]byte(data)))
	if err != nil {
		t.Fatalf("Resolve(%q): %v", data, err)
	}
	return id
}

func mustAdd(t *testing.T, db *DB, repo string, id *astral.ObjectID) {
	t.Helper()

	if err := db.addToRepo(repo, id); err != nil {
		t.Fatalf("addToRepo(%q, %v): %v", repo, id, err)
	}
}

func mustRemove(t *testing.T, db *DB, repo string, id *astral.ObjectID) {
	t.Helper()

	if err := db.removeFromRepo(repo, id); err != nil {
		t.Fatalf("removeFromRepo(%q, %v): %v", repo, id, err)
	}
}

func requireChange(t *testing.T, db *DB, repo string, after uint64, wantVersion uint64, wantID *astral.ObjectID, wantExist bool) {
	t.Helper()

	row, err := db.nextChange(repo, after)
	if err != nil {
		t.Fatalf("nextChange(%q, %d): %v", repo, after, err)
	}
	if row == nil {
		t.Fatalf("nextChange(%q, %d): got nil, want version %d", repo, after, wantVersion)
	}
	if row.Version != wantVersion || row.ObjectID == nil || *row.ObjectID != *wantID || row.Exist != wantExist {
		t.Fatalf("nextChange(%q, %d): got version %d id %v exist %v, want version %d id %v exist %v",
			repo, after, row.Version, row.ObjectID, row.Exist, wantVersion, wantID, wantExist)
	}
}

func idStrings(ids []*astral.ObjectID) []string {
	var s []string
	for _, id := range ids {
		s = append(s, id.String())
	}
	return s
}

func TestDB_AddAndRemoveRejectRepeats(t *testing.T) {
	db := testChangelogDB(t)
	a := changelogID(t, "a")
	c := changelogID(t, "c")

	mustAdd(t, db, "r", a)

	if err := db.addToRepo("r", a); !errors.Is(err, indexing.ErrObjectAlreadyAdded) {
		t.Errorf("second addToRepo: got %v, want %v", err, indexing.ErrObjectAlreadyAdded)
	}
	if err := db.removeFromRepo("r", c); !errors.Is(err, indexing.ErrObjectNotPresent) {
		t.Errorf("removeFromRepo of an object never added: got %v, want %v", err, indexing.ErrObjectNotPresent)
	}

	mustRemove(t, db, "r", a)

	if err := db.removeFromRepo("r", a); !errors.Is(err, indexing.ErrObjectNotPresent) {
		t.Errorf("second removeFromRepo: got %v, want %v", err, indexing.ErrObjectNotPresent)
	}
}

func TestDB_ChangelogVersions(t *testing.T) {
	db := testChangelogDB(t)
	a := changelogID(t, "a")
	b := changelogID(t, "b")
	c := changelogID(t, "c")

	mustAdd(t, db, "r", a)
	mustAdd(t, db, "r", b)
	mustRemove(t, db, "r", a)

	requireChange(t, db, "r", 0, 1, a, true)
	requireChange(t, db, "r", 1, 2, b, true)
	requireChange(t, db, "r", 2, 3, a, false)

	row, err := db.nextChange("r", 3)
	if err != nil || row != nil {
		t.Fatalf("nextChange(\"r\", 3): got %v, %v, want nil, nil", row, err)
	}

	if exists, err := db.latestExists("r", a); err != nil || exists {
		t.Errorf("latestExists(\"r\", a) after remove: got %v, %v, want false, nil", exists, err)
	}
	if exists, err := db.latestExists("r", b); err != nil || !exists {
		t.Errorf("latestExists(\"r\", b): got %v, %v, want true, nil", exists, err)
	}

	existing, err := db.latestExistingObjectIDs("r")
	if err != nil {
		t.Fatalf("latestExistingObjectIDs: %v", err)
	}
	if got := idStrings(existing); len(got) != 1 || got[0] != b.String() {
		t.Errorf("latestExistingObjectIDs(\"r\"): got %v, want [%v]", got, b)
	}

	excess, err := db.findExcessObjectIDs("r", []*astral.ObjectID{c})
	if err != nil {
		t.Fatalf("findExcessObjectIDs: %v", err)
	}
	if got := idStrings(excess); len(got) != 1 || got[0] != b.String() {
		t.Errorf("findExcessObjectIDs(\"r\", [c]): got %v, want [%v]", got, b)
	}

	missing, err := db.findMissingObjectIDs("r", []*astral.ObjectID{b, c})
	if err != nil {
		t.Fatalf("findMissingObjectIDs: %v", err)
	}
	if got := idStrings(missing); len(got) != 1 || got[0] != c.String() {
		t.Errorf("findMissingObjectIDs(\"r\", [b c]): got %v, want [%v]", got, c)
	}

	mustAdd(t, db, "r", a)

	if exists, err := db.latestExists("r", a); err != nil || !exists {
		t.Errorf("latestExists(\"r\", a) after re-add: got %v, %v, want true, nil", exists, err)
	}
	requireChange(t, db, "r", 3, 4, a, true)
}

func TestDB_VersionsArePerRepo(t *testing.T) {
	db := testChangelogDB(t)
	a := changelogID(t, "a")
	b := changelogID(t, "b")

	mustAdd(t, db, "r", a)
	mustAdd(t, db, "r", b)
	mustAdd(t, db, "s", a)

	requireChange(t, db, "s", 0, 1, a, true)

	row, err := db.nextChange("s", 1)
	if err != nil || row != nil {
		t.Fatalf("nextChange(\"s\", 1): got %v, %v, want nil, nil", row, err)
	}
}
