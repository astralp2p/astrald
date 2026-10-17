package indexing

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/indexing"
	"github.com/astralp2p/astrald/mod/objects"
)

// listedRepository is a repository whose scan lists snapshot, sends the snapshot boundary,
// sends follow and ends.
type listedRepository struct {
	objects.Repository
	snapshot []*astral.ObjectID
	follow   []*astral.ObjectID
}

func (r listedRepository) Scan(*astral.Context, bool) (<-chan *astral.ObjectID, error) {
	ch := make(chan *astral.ObjectID, len(r.snapshot)+len(r.follow)+1)
	for _, id := range r.snapshot {
		ch <- id
	}
	ch <- nil
	for _, id := range r.follow {
		ch <- id
	}
	close(ch)
	return ch, nil
}

type listedObjects struct {
	objects.Module
	repo listedRepository
}

func (o listedObjects) GetRepository(string) objects.Repository { return o.repo }

// syncListed runs one sync of repo against db and returns once the scan ends.
func syncListed(t *testing.T, db *DB, repo listedRepository) {
	t.Helper()

	ctx, cancel := astral.NewContext(nil).WithTimeout(30 * time.Second)
	defer cancel()

	mod := &Module{Deps: Deps{Objects: listedObjects{repo: repo}}, db: db, log: &log.Logger{}}
	if err := mod.syncRepo(ctx, "music"); err != nil {
		t.Fatalf("syncRepo: %v", err)
	}
}

func requireNoChange(t *testing.T, db *DB, repo string, after uint64) {
	t.Helper()

	row, err := db.nextChange(repo, after)
	if err != nil {
		t.Fatalf("nextChange(%q, %d): %v", repo, after, err)
	}
	if row != nil {
		t.Fatalf("nextChange(%q, %d): got version %d id %v exist %v, want none", repo, after, row.Version, row.ObjectID, row.Exist)
	}
}

func TestDB_AddRefusesTheZeroObjectID(t *testing.T) {
	db := testChangelogDB(t)

	for _, id := range []*astral.ObjectID{nil, {}} {
		if err := db.addToRepo("r", id); !errors.Is(err, indexing.ErrZeroObjectID) {
			t.Errorf("addToRepo(%v): got %v, want %v", id, err, indexing.ErrZeroObjectID)
		}
	}
	requireNoChange(t, db, "r", 0)
}

// TestSyncRepoAddsNoZeroObjectID: a zero ObjectID in the snapshot or in the followed updates
// adds no changelog entry, and the objects around it are still added.
func TestSyncRepoAddsNoZeroObjectID(t *testing.T) {
	db := testChangelogDB(t)
	a := changelogID(t, "a")
	b := changelogID(t, "b")

	syncListed(t, db, listedRepository{
		snapshot: []*astral.ObjectID{{}, a},
		follow:   []*astral.ObjectID{{}, b},
	})

	requireChange(t, db, "music", 0, 1, a, true)
	requireChange(t, db, "music", 1, 2, b, true)
	requireNoChange(t, db, "music", 2)
}

// TestSyncRepoRemovesAStoredZeroObjectID: a zero ObjectID stored before the refusal is
// recorded as removed by a sync whose snapshot lacks it.
func TestSyncRepoRemovesAStoredZeroObjectID(t *testing.T) {
	db := testChangelogDB(t)
	a := changelogID(t, "a")
	zero := &astral.ObjectID{}

	err := db.Create(&dbRepoEntry{Repo: "music", Version: 1, ObjectID: zero, Exist: true}).Error
	if err != nil {
		t.Fatalf("store zero entry: %v", err)
	}

	syncListed(t, db, listedRepository{snapshot: []*astral.ObjectID{a}})

	requireChange(t, db, "music", 1, 2, zero, false)
	requireChange(t, db, "music", 2, 3, a, true)
	requireNoChange(t, db, "music", 3)
}
