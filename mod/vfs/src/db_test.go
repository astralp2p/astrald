package vfs

import (
	"errors"
	"strings"
	"testing"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

func TestTables(t *testing.T) {
	mod := testModule(t)

	for _, table := range []string{"vfs__volumes", "vfs__entries"} {
		if !mod.db.Migrator().HasTable(table) {
			t.Errorf("table %s missing", table)
		}
	}

	var sqls []string
	if err := mod.db.Raw("SELECT sql FROM sqlite_master WHERE tbl_name = 'vfs__entries'").Scan(&sqls).Error; err != nil {
		t.Fatalf("read schema: %v", err)
	}
	schema := strings.ToLower(strings.Join(sqls, "\n"))

	for _, want := range []string{
		"collate binary", // the name column
		"unique index `idx_vfs__entries_parent_name`", // unique on parent and name
		"`idx_vfs__entries_object_id`",                // indexed on ObjectID
		"`idx_vfs__entries_vfs_object`",               // indexed on VFS with ObjectID
	} {
		if !strings.Contains(schema, want) {
			t.Errorf("schema lacks %q:\n%s", want, schema)
		}
	}
}

func TestEntryUniqueOnParentAndName(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	addFile(t, mod, vol, vol.RootID, "a", "x")

	// a name differs by case and by byte, so these coexist
	addFile(t, mod, vol, vol.RootID, "A", "x")
	addFile(t, mod, vol, vol.RootID, "é", "x")

	dup := &dbEntry{VFS: vol.Identity, ParentID: vol.RootID, Name: "a", ModTime: testModTime}
	if err := mod.db.Create(dup).Error; err == nil {
		t.Fatal("a second entry named a in the same folder was inserted")
	}

	folder := addFolder(t, mod, vol, vol.RootID, "dir")
	addFile(t, mod, vol, folder.ID, "a", "x") // same name, other parent
}

func TestRootRowIsItsOwnParent(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	root, err := mod.db.getEntry(vol.RootID)
	if err != nil {
		t.Fatalf("getEntry: %v", err)
	}
	if !root.isRoot() || root.ParentID != root.ID {
		t.Fatalf("root row %d has parent %d", root.ID, root.ParentID)
	}
	if !root.isFolder() {
		t.Fatal("root row is not a folder")
	}
	if root.ObjectID != nil {
		t.Fatalf("a new root has a cached ObjectID %v", root.ObjectID)
	}

	kids, err := mod.db.listChildren(root.ID)
	if err != nil || len(kids) != 0 {
		t.Fatalf("listChildren of an empty root = %v, %v", kids, err)
	}
}

func TestFindVolume(t *testing.T) {
	mod := testModule(t)
	id := astral.GenerateIdentity()

	if _, err := mod.db.findVolume(id); !errors.Is(err, vfsapi.ErrNoSuchVFS) {
		t.Fatalf("findVolume of an unknown identity: %v, want no such vfs", err)
	}

	want := createVFS(t, mod, id)
	got, err := mod.db.findVolume(id)
	if err != nil || got.ID != want.ID {
		t.Fatalf("findVolume = %v, %v", got, err)
	}

	if _, err := mod.db.findVolume(astral.GenerateIdentity()); !errors.Is(err, vfsapi.ErrNoSuchVFS) {
		t.Fatalf("findVolume of another identity: %v, want no such vfs", err)
	}
}

func TestListChildrenOrder(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	for _, name := range []string{"z", "é", "B", "è", "a", "A"} {
		addFile(t, mod, vol, vol.RootID, name, name)
	}

	kids, err := mod.db.listChildren(vol.RootID)
	if err != nil {
		t.Fatalf("listChildren: %v", err)
	}

	var got []string
	for _, k := range kids {
		got = append(got, k.Name)
	}
	if want := "A B a z è é"; strings.Join(got, " ") != want {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestResolveEntry(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	dir := addFolder(t, mod, vol, vol.RootID, "dir")
	file := addFile(t, mod, vol, dir.ID, "f", "x")

	tests := []struct {
		names   []string
		wantRow int64
		wantErr error
	}{
		{nil, vol.RootID, nil},
		{[]string{"dir"}, dir.ID, nil},
		{[]string{"dir", "f"}, file.ID, nil},
		{[]string{"missing"}, 0, vfsapi.ErrNotFound},
		{[]string{"dir", "missing"}, 0, vfsapi.ErrNotFound},
		{[]string{"dir", "f", "x"}, 0, vfsapi.ErrNotAFolder},
		{[]string{"dir", "f", "x", "y"}, 0, vfsapi.ErrNotAFolder},
	}

	for _, tt := range tests {
		row, err := mod.db.resolveEntry(vol, tt.names)
		if !errors.Is(err, tt.wantErr) {
			t.Errorf("resolveEntry(%v) error = %v, want %v", tt.names, err, tt.wantErr)
			continue
		}
		if err == nil && row.ID != tt.wantRow {
			t.Errorf("resolveEntry(%v) = row %d, want %d", tt.names, row.ID, tt.wantRow)
		}
	}
}

func TestResolveEntryIsolatedPerVFS(t *testing.T) {
	mod := testModule(t)
	a := createVFS(t, mod, astral.GenerateIdentity())
	b := createVFS(t, mod, astral.GenerateIdentity())

	addFile(t, mod, a, a.RootID, "only-in-a", "x")

	if _, err := mod.db.resolveEntry(b, []string{"only-in-a"}); !errors.Is(err, vfsapi.ErrNotFound) {
		t.Fatalf("resolveEntry in the other VFS: %v, want not found", err)
	}
}

func TestUsageCountsDistinctObjectIDsOnce(t *testing.T) {
	mod := testModule(t)
	id := astral.GenerateIdentity()
	vol := createVFS(t, mod, id)
	other := createVFS(t, mod, astral.GenerateIdentity())

	if got := usageOf(t, mod, id); got != 0 {
		t.Fatalf("usage of an empty VFS = %d", got)
	}

	dir := addFolder(t, mod, vol, vol.RootID, "dir")
	addFile(t, mod, vol, vol.RootID, "a", "hello")         // 5
	addFile(t, mod, vol, dir.ID, "same", "hello")          // 5, same content: counted once
	addFile(t, mod, vol, dir.ID, "b", "hello world")       // 11
	addFile(t, mod, vol, dir.ID, "empty", "")              // 0
	addFile(t, mod, other, other.RootID, "c", "elsewhere") // another VFS

	if got, want := usageOf(t, mod, id), uint64(16); got != want {
		t.Fatalf("usage = %d, want %d", got, want)
	}
	if got, want := usageOf(t, mod, other.Identity), uint64(9); got != want {
		t.Fatalf("usage of the other VFS = %d, want %d", got, want)
	}
}

func TestEntryCountIsPerVFSAndSkipsTheRoot(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	other := createVFS(t, mod, astral.GenerateIdentity())

	if got := countOf(t, mod, vol.Identity); got != 0 {
		t.Fatalf("entries of an empty VFS = %d", got)
	}

	dir := addFolder(t, mod, vol, vol.RootID, "dir")
	addFile(t, mod, vol, dir.ID, "a", "x")
	addFile(t, mod, vol, dir.ID, "b", "x")
	addFile(t, mod, other, other.RootID, "c", "x")

	if got, want := countOf(t, mod, vol.Identity), uint64(3); got != want {
		t.Fatalf("entries = %d, want %d (folders count, the root does not)", got, want)
	}
	if got, want := countOf(t, mod, other.Identity), uint64(1); got != want {
		t.Fatalf("entries of the other VFS = %d, want %d", got, want)
	}
}

func usageOf(t *testing.T, mod *Module, id *astral.Identity) uint64 {
	t.Helper()

	n, err := mod.db.usage(id)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}

	return n
}

func countOf(t *testing.T, mod *Module, id *astral.Identity) uint64 {
	t.Helper()

	n, err := mod.db.entryCount(id)
	if err != nil {
		t.Fatalf("entryCount: %v", err)
	}

	return n
}
