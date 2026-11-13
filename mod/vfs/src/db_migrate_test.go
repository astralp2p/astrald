package vfs

import (
	"testing"
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

// dbEntryBeforeCreationTime is the entries table as it was before the creation_time column.
type dbEntryBeforeCreationTime struct {
	ID       int64            `gorm:"primaryKey"`
	VFS      *astral.Identity `gorm:"not null"`
	ParentID int64            `gorm:"not null"`
	Name     string           `gorm:"type:text collate binary;not null"`
	Flags    uint16           `gorm:"not null"`
	ObjectID *astral.ObjectID
	ModTime  int64 `gorm:"not null"`
}

func (dbEntryBeforeCreationTime) TableName() string { return dbEntry{}.TableName() }

// oldSchemaFixture is a VFS whose entries table has the schema before the creation_time column: a root and a
// folder with stale cached ObjectIDs, and two files.
type oldSchemaFixture struct {
	mod                *Module
	vol                *dbVolume
	sub, fileA, fileB  int64
	subTime, fileATime int64
}

func newOldSchemaFixture(t *testing.T) *oldSchemaFixture {
	t.Helper()

	f := &oldSchemaFixture{mod: testModule(t), subTime: testModTime + 1, fileATime: testModTime + 2}
	f.vol = createVFS(t, f.mod, astral.GenerateIdentity())

	m := f.mod.db.Migrator()
	if err := m.DropTable(&dbEntry{}); err != nil {
		t.Fatal(err)
	}
	if err := m.CreateTable(&dbEntryBeforeCreationTime{}); err != nil {
		t.Fatal(err)
	}

	stale := contentID("a stale cached folder ObjectID")
	f.insert(t, &dbEntryBeforeCreationTime{ID: f.vol.RootID, ParentID: f.vol.RootID, Flags: uint16(vfsapi.FlagFolder), ObjectID: &stale, ModTime: testModTime})
	f.sub = f.insert(t, &dbEntryBeforeCreationTime{ParentID: f.vol.RootID, Name: "sub", Flags: uint16(vfsapi.FlagFolder), ObjectID: &stale, ModTime: f.subTime})
	a, b := contentID("a"), contentID("b")
	f.fileA = f.insert(t, &dbEntryBeforeCreationTime{ParentID: f.vol.RootID, Name: "a", ObjectID: &a, ModTime: f.fileATime})
	f.fileB = f.insert(t, &dbEntryBeforeCreationTime{ParentID: f.sub, Name: "b", ObjectID: &b, ModTime: testModTime})

	return f
}

func (f *oldSchemaFixture) insert(t *testing.T, row *dbEntryBeforeCreationTime) int64 {
	t.Helper()

	row.VFS = f.vol.Identity
	if err := f.mod.db.Create(row).Error; err != nil {
		t.Fatalf("insert %q: %v", row.Name, err)
	}

	return row.ID
}

func (f *oldSchemaFixture) migrate(t *testing.T) {
	t.Helper()

	if err := f.mod.db.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

func (f *oldSchemaFixture) row(t *testing.T, id int64) *dbEntry {
	t.Helper()

	row, err := f.mod.db.findEntry(id)
	if err != nil {
		t.Fatalf("find row %d: %v", id, err)
	}

	return row
}

// expectedRootID is the ObjectID of the root after the migration, computed with the SDK writer.
func (f *oldSchemaFixture) expectedRootID(t *testing.T) astral.ObjectID {
	t.Helper()

	at := func(ns int64) astral.Time { return astral.Time(time.Unix(0, ns).UTC()) }
	subID := expectedFolderID(t, &vfsapi.Entry{Name: "b", ID: contentID("b"), ModTime: at(testModTime), CreationTime: at(testModTime)})

	return expectedFolderID(t,
		&vfsapi.Entry{Name: "a", ID: contentID("a"), ModTime: at(f.fileATime), CreationTime: at(f.fileATime)},
		&vfsapi.Entry{Name: "sub", ID: subID, Flags: vfsapi.FlagFolder, ModTime: at(f.subTime), CreationTime: at(f.subTime)},
	)
}

func TestMigrateCreationTimeBackfillsAndClearsFolderIDs(t *testing.T) {
	f := newOldSchemaFixture(t)

	f.migrate(t)

	for _, id := range []int64{f.sub, f.fileA, f.fileB} {
		if row := f.row(t, id); row.CreationTime != row.ModTime {
			t.Errorf("%s: creation time %d, want its mod time %d", row.Name, row.CreationTime, row.ModTime)
		}
	}
	if got := f.row(t, f.vol.RootID).CreationTime; got != 0 {
		t.Errorf("root creation time %d, want 0", got)
	}
	for _, id := range []int64{f.vol.RootID, f.sub} {
		if got := f.row(t, id).ObjectID; got != nil {
			t.Errorf("folder row %d kept the cached ObjectID %v", id, got)
		}
	}
	if got := f.row(t, f.fileA).ObjectID; got == nil || *got != contentID("a") {
		t.Errorf("file ObjectID %v, want its content", got)
	}

	got, err := f.mod.folderObjectID(f.vol.Identity, f.vol.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if want := f.expectedRootID(t); got != want {
		t.Errorf("root ObjectID %v, want %v under mod.vfs.directory", got, want)
	}
}

func TestMigrateCreationTimeRunsOnce(t *testing.T) {
	f := newOldSchemaFixture(t)
	f.migrate(t)

	cached, err := f.mod.folderObjectID(f.vol.Identity, f.vol.RootID)
	if err != nil {
		t.Fatal(err)
	}
	const later int64 = 1800000000000000000
	if err := f.mod.db.Model(&dbEntry{}).Where("id = ?", f.fileB).Update("creation_time", later).Error; err != nil {
		t.Fatal(err)
	}

	f.migrate(t)

	if got := f.row(t, f.vol.RootID).ObjectID; got == nil || *got != cached {
		t.Errorf("a second start changed the cached root ObjectID to %v, want %v", got, cached)
	}
	if got := f.row(t, f.sub).ObjectID; got == nil {
		t.Error("a second start cleared the cached folder ObjectID")
	}
	if got := f.row(t, f.fileB).CreationTime; got != later {
		t.Errorf("a second start set the creation time to %d, want %d", got, later)
	}
}
