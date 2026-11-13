package objects

import (
	"testing"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/objects/mem"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newTrackingModule returns a Module with a migrated in-memory database and the default repositories.
func newTrackingModule(t *testing.T) *Module {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	// note: an sqlite :memory: database is per connection; one connection keeps every call on the same database.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })

	db := &DB{DB: gdb}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	lg := log.New(astral.GenerateIdentity())
	mod := &Module{config: defaultConfig, db: db, log: lg}
	mod.objectsReadsJournal = newObjectsReadsJournal(db.UpdateReadAt, lg)
	if err := mod.setupDefaultRepos(); err != nil {
		t.Fatalf("setup default repositories: %v", err)
	}
	return mod
}

// commitData writes data to repo and commits it.
func commitData(t *testing.T, ctx *astral.Context, repo objectsmod.Repository, data string) *astral.ObjectID {
	t.Helper()

	w, err := repo.Create(ctx, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer w.Discard()

	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatalf("write: %v", err)
	}
	id, err := w.Commit()
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return id
}

// trackingRows returns every tracking row of the leaf named repo.
func trackingRows(t *testing.T, mod *Module, repo string) []*dbTracking {
	t.Helper()

	var rows []*dbTracking
	if err := mod.db.Where("repository = ?", repo).Find(&rows).Error; err != nil {
		t.Fatalf("list tracking rows: %v", err)
	}
	return rows
}

// expectTracked fails unless the leaf named repo has exactly one row, for id, and returns it.
func expectTracked(t *testing.T, mod *Module, repo string, id *astral.ObjectID) *dbTracking {
	t.Helper()

	rows := trackingRows(t, mod, repo)
	if len(rows) != 1 || !rows[0].ObjectID.IsEqual(id) {
		t.Fatalf("tracking rows of %v: %v; want one row for %v", repo, rows, id)
	}
	return rows[0]
}

// TestTracking_SystemWriteCreatesRow: System() returns the wrapper, so a write through it is tracked.
func TestTracking_SystemWriteCreatesRow(t *testing.T) {
	mod := newTrackingModule(t)

	if _, ok := mod.System().(*trackedRepository); !ok {
		t.Fatalf("System() returned %T; want the tracking wrapper", mod.System())
	}

	before := astral.Now().Time().Unix()
	id := commitData(t, astral.NewContext(nil), mod.System(), "system data")

	row := expectTracked(t, mod, objects.RepoSystem, id)
	if row.CommittedAt < before {
		t.Errorf("commit time %d; want at least %d", row.CommittedAt, before)
	}
	if row.ReadAt != 0 {
		t.Errorf("read time %d; want 0 before any counted read", row.ReadAt)
	}
}

// TestTracking_AddRepositoryWrapsLeaf: AddRepository registers the wrapper, which exposes the inner repository.
func TestTracking_AddRepositoryWrapsLeaf(t *testing.T) {
	mod := newTrackingModule(t)
	inner := mem.New("leaf", 0)

	if err := mod.AddRepository("leaf", inner); err != nil {
		t.Fatalf("AddRepository: %v", err)
	}
	if err := mod.AddRepository("leaf", inner); err == nil {
		t.Fatal("a second AddRepository under one name succeeded")
	}

	tracked, ok := mod.GetRepository("leaf").(*trackedRepository)
	if !ok {
		t.Fatalf("registered %T; want the tracking wrapper", mod.GetRepository("leaf"))
	}
	if tracked.Inner() != inner {
		t.Fatal("Inner() does not return the registered repository")
	}

	id := commitData(t, astral.NewContext(nil), tracked, "leaf data")
	expectTracked(t, mod, "leaf", id)
}

// TestTracking_CommitOfExistingContentRefreshesTime: a commit counts also when the content already exists.
func TestTracking_CommitOfExistingContentRefreshesTime(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	repo := mod.GetRepository("mem0")

	id := commitData(t, ctx, repo, "same content")
	err := mod.db.Model(&dbTracking{}).Where("repository = ?", "mem0").Update("committed_at", 1).Error
	if err != nil {
		t.Fatalf("age the row: %v", err)
	}

	before := astral.Now().Time().Unix()
	again := commitData(t, ctx, repo, "same content")
	if !again.IsEqual(id) {
		t.Fatalf("second commit answered %v; want %v", again, id)
	}

	row := expectTracked(t, mod, "mem0", id)
	if row.CommittedAt < before {
		t.Errorf("commit time %d after a second commit; want at least %d", row.CommittedAt, before)
	}
}

// TestTracking_DeleteRemovesRow: a successful delete removes the row, also for a partial ID.
func TestTracking_DeleteRemovesRow(t *testing.T) {
	cases := []struct {
		name    string
		request func(full *astral.ObjectID) *astral.ObjectID
	}{
		{"full", func(full *astral.ObjectID) *astral.ObjectID { return full }},
		{"partial", func(full *astral.ObjectID) *astral.ObjectID { return &astral.ObjectID{Hash: full.Hash} }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mod := newTrackingModule(t)
			ctx := astral.NewContext(nil)
			repo := mod.GetRepository("mem0")

			kept := commitData(t, ctx, repo, "kept")
			id := commitData(t, ctx, repo, "deleted")

			if err := repo.Delete(ctx, c.request(id)); err != nil {
				t.Fatalf("delete: %v", err)
			}
			expectTracked(t, mod, "mem0", kept)

			if err := repo.Delete(ctx, id); err == nil {
				t.Fatal("a second delete succeeded")
			}
			expectTracked(t, mod, "mem0", kept)
		})
	}
}

// afterRemovedLeaf is a leaf that records the name AfterRemoved was called with.
type afterRemovedLeaf struct {
	*mem.Repository
	removed []string
}

func (r *afterRemovedLeaf) AfterRemoved(name string) {
	r.removed = append(r.removed, name)
}

// TestTracking_RemoveRepositoryRemovesRows: removing a leaf removes its rows only and forwards AfterRemoved.
func TestTracking_RemoveRepositoryRemovesRows(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	leaf := &afterRemovedLeaf{Repository: mem.New("leaf", 0)}
	if err := mod.AddRepository("leaf", leaf); err != nil {
		t.Fatalf("AddRepository: %v", err)
	}

	commitData(t, ctx, mod.GetRepository("leaf"), "one")
	commitData(t, ctx, mod.GetRepository("leaf"), "two")
	other := commitData(t, ctx, mod.GetRepository("mem0"), "one")

	if err := mod.RemoveRepository("leaf"); err != nil {
		t.Fatalf("RemoveRepository: %v", err)
	}

	if rows := trackingRows(t, mod, "leaf"); len(rows) != 0 {
		t.Fatalf("rows of a removed leaf: %v; want none", rows)
	}
	expectTracked(t, mod, "mem0", other)
	if len(leaf.removed) != 1 || leaf.removed[0] != "leaf" {
		t.Fatalf("AfterRemoved calls %v; want [leaf]", leaf.removed)
	}
}

// TestTracking_GetRepoNameFindsWrappedLeaf: a probe through the leaf or a group names the registered leaf.
func TestTracking_GetRepoNameFindsWrappedLeaf(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	id := commitData(t, ctx, mod.GetRepository("mem0"), "named")

	for _, via := range []string{"mem0", objects.RepoMemory} {
		probe, err := mod.Probe(ctx, mod.GetRepository(via), id)
		if err != nil {
			t.Fatalf("probe via %v: %v", via, err)
		}
		if string(probe.Repo) != "mem0" {
			t.Errorf("probe via %v named repo %q; want mem0", via, probe.Repo)
		}
	}
}

// TestTracking_HousekeepingCommitIsTracked: the housekeeping mark affects reads only, so a marked commit still writes its row.
func TestTracking_HousekeepingCommitIsTracked(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := objectsmod.WithHousekeeping(astral.NewContext(nil))

	id := commitData(t, ctx, mod.GetRepository("mem0"), "housekeeping write")
	expectTracked(t, mod, "mem0", id)
}

// TestTracking_ReadCreatesNoRow: a read of an untracked object leaves the tracking table empty.
func TestTracking_ReadCreatesNoRow(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	inner := mem.New("leaf", 0)
	id := commitData(t, ctx, inner, "untracked")
	if err := mod.AddRepository("leaf", inner); err != nil {
		t.Fatalf("AddRepository: %v", err)
	}

	if _, err := mod.Load(ctx, mod.GetRepository("leaf"), id); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := mod.Probe(ctx, mod.GetRepository("leaf"), id); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if rows := trackingRows(t, mod, "leaf"); len(rows) != 0 {
		t.Fatalf("rows after reads: %v; want none", rows)
	}
}

// housekeepingLeaf is a leaf that records whether each Read ran under the housekeeping mark.
type housekeepingLeaf struct {
	*mem.Repository
	marks []bool
}

func (r *housekeepingLeaf) Read(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	r.marks = append(r.marks, objectsmod.IsHousekeeping(ctx))
	return r.Repository.Read(ctx, objectID, offset, limit)
}

// TestTracking_ProbeMarksHousekeeping: Probe reads under the housekeeping mark, whoever asks; Load does not.
func TestTracking_ProbeMarksHousekeeping(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	leaf := &housekeepingLeaf{Repository: mem.New("leaf", 0)}
	id := commitData(t, ctx, leaf, "probed")

	if _, err := mod.Probe(ctx, leaf, id); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if _, err := mod.Load(ctx, leaf, id); err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(leaf.marks) != 2 || !leaf.marks[0] || leaf.marks[1] {
		t.Fatalf("housekeeping marks of probe and load: %v; want [true false]", leaf.marks)
	}
	if objectsmod.IsHousekeeping(ctx) {
		t.Fatal("Probe marked the caller's context")
	}
}

// TestTracking_MigrateDropsOldTable: Migrate drops the old tracking table.
func TestTracking_MigrateDropsOldTable(t *testing.T) {
	mod := newTrackingModule(t)

	if err := mod.db.Exec("CREATE TABLE " + dbObjectsTableName + " (id TEXT)").Error; err != nil {
		t.Fatalf("create old table: %v", err)
	}
	if err := mod.db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if mod.db.Migrator().HasTable(dbObjectsTableName) {
		t.Fatalf("%v survived Migrate", dbObjectsTableName)
	}
	if !mod.db.Migrator().HasTable(&dbTracking{}) {
		t.Fatal("the tracking table is missing after Migrate")
	}
}

// TestTracking_SetupFailsWhenADefaultLeafCannotBeAdded: a name already taken by mem0 or system fails the setup
// that registers the default leaves.
func TestTracking_SetupFailsWhenADefaultLeafCannotBeAdded(t *testing.T) {
	for _, name := range []string{"mem0", objects.RepoSystem} {
		mod := &Module{config: defaultConfig}
		mod.repos.Set(name, mem.New("taken", 0))

		if err := mod.setupDefaultRepos(); err == nil {
			t.Errorf("setup with %v taken succeeded; want an error", name)
		}
	}
}
