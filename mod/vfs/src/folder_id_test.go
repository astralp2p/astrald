package vfs

import (
	"encoding/hex"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// The vectors of VFS 6, section 5, with the type name mod.vfs.directory and the creation time. Every ModTime is
// 1700000000000000000 and every CreationTime 1699996400000000000.
const (
	v1Hash = "5bda020d3782b67b601ab110d94f3a3d64396438e5b4c4a7c2cbf3d9ff565382"
	v2Hash = "d7702ed2ff919b922b0b391123f977fe466455a7afcaba17f0528e66f0bc3982"
	v3Hash = "dab8a151d3e876ddb3c9cac823a0078a6a715698c82ae5b3b85b8d4abbb2805d"
	v4Hash = "d34da3f6c22c6532dc7737e39aca0b641256797c324fd7273435226586135de5"
)

// The sizes of the vectors in bytes.
const (
	v1Size = 26
	v2Size = 90
	v3Size = 88
	v4Size = 208
)

func requireFolderID(t *testing.T, mod *Module, vol *dbVolume, row int64, size uint64, hash string) {
	t.Helper()

	id, err := mod.folderObjectID(vol.Identity, row)
	if err != nil {
		t.Fatalf("folderObjectID: %v", err)
	}
	if id.Size != size || hex.EncodeToString(id.Hash[:]) != hash {
		t.Fatalf("ObjectID = size %d %x, want size %d %s", id.Size, id.Hash, size, hash)
	}
}

// TestVectorV1EmptyFolder: the root of a new VFS is the empty folder.
func TestVectorV1EmptyFolder(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	requireFolderID(t, mod, vol, vol.RootID, v1Size, v1Hash)
}

func TestVectorV2OneFile(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	addFile(t, mod, vol, vol.RootID, "a.txt", "hello\n")

	requireFolderID(t, mod, vol, vol.RootID, v2Size, v2Hash)
}

func TestVectorV3OneSubfolder(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	sub := addFolder(t, mod, vol, vol.RootID, "sub")

	requireFolderID(t, mod, vol, vol.RootID, v3Size, v3Hash)

	// the empty sub-folder is V1, and the computation cached it
	requireFolderID(t, mod, vol, sub.ID, v1Size, v1Hash)
}

func TestVectorV4ByteOrderOfNames(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	// inserted out of order on purpose
	addFile(t, mod, vol, vol.RootID, "é", "3")
	addFile(t, mod, vol, vol.RootID, "z", "1")
	addFile(t, mod, vol, vol.RootID, "è", "2")

	requireFolderID(t, mod, vol, vol.RootID, v4Size, v4Hash)
}

func TestComputeCachesEveryFolderItFills(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	a := addFolder(t, mod, vol, vol.RootID, "a")
	b := addFolder(t, mod, vol, a.ID, "b")
	addFile(t, mod, vol, b.ID, "f", "x")

	rootID, err := mod.folderObjectID(vol.Identity, vol.RootID)
	if err != nil {
		t.Fatalf("folderObjectID: %v", err)
	}

	for _, row := range []int64{vol.RootID, a.ID, b.ID} {
		got, err := mod.db.findEntry(row)
		if err != nil || got.ObjectID == nil {
			t.Fatalf("row %d not cached: %v, %v", row, got, err)
		}
	}

	root, _ := mod.db.findEntry(vol.RootID)
	if *root.ObjectID != rootID {
		t.Fatalf("cached root %v, computed %v", root.ObjectID, rootID)
	}
}

func TestComputeUsesCachedFolderIDs(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	sub := addFolder(t, mod, vol, vol.RootID, "sub")

	// a planted cache is trusted until it is cleared
	planted := contentID("planted")
	if err := mod.db.Model(&dbEntry{}).Where("id = ?", sub.ID).Update("object_id", &planted).Error; err != nil {
		t.Fatalf("plant: %v", err)
	}

	root, err := mod.db.findEntry(vol.RootID)
	if err != nil {
		t.Fatalf("findEntry: %v", err)
	}
	plan, err := planFolder(mod.db, vol.Identity, root, 0)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	id := plan.ID
	if len(plan.Dirs) != 1 || plan.Dirs[0].Row != vol.RootID {
		t.Fatalf("planned = %v, want the root only", plan.Dirs)
	}

	if err := mod.db.clearFolderIDs(sub.ID, 0); err != nil {
		t.Fatalf("clearFolderIDs: %v", err)
	}
	root, err = mod.db.findEntry(vol.RootID)
	if err != nil {
		t.Fatalf("findEntry: %v", err)
	}
	plan, err = planFolder(mod.db, vol.Identity, root, 0)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	fresh := plan.ID
	if fresh == id {
		t.Fatal("the directory did not change after the cache was cleared")
	}
	if got := hex.EncodeToString(fresh.Hash[:]); got != v3Hash {
		t.Fatalf("root after clearing = %s, want V3 %s", got, v3Hash)
	}
}

func TestClearFolderIDsClearsTheFolderAndEveryAncestor(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	other := createVFS(t, mod, astral.GenerateIdentity())

	a := addFolder(t, mod, vol, vol.RootID, "a")
	b := addFolder(t, mod, vol, a.ID, "b")
	c := addFolder(t, mod, vol, b.ID, "c")
	side := addFolder(t, mod, vol, vol.RootID, "side")
	sideKid := addFolder(t, mod, vol, a.ID, "kid")
	file := addFile(t, mod, vol, c.ID, "f", "x")

	for _, v := range []*dbVolume{vol, other} {
		if _, err := mod.folderObjectID(v.Identity, v.RootID); err != nil {
			t.Fatalf("folderObjectID: %v", err)
		}
	}

	if err := mod.db.clearFolderIDs(b.ID, 0); err != nil {
		t.Fatalf("clearFolderIDs: %v", err)
	}

	cleared := map[string]int64{"root": vol.RootID, "a": a.ID, "b": b.ID}
	for name, row := range cleared {
		if got, _ := mod.db.findEntry(row); got.ObjectID != nil {
			t.Errorf("%s still has a cached ObjectID", name)
		}
	}

	kept := map[string]int64{"c": c.ID, "side": side.ID, "kid": sideKid.ID, "other root": other.RootID}
	for name, row := range kept {
		if got, _ := mod.db.findEntry(row); got.ObjectID == nil {
			t.Errorf("%s lost its cached ObjectID", name)
		}
	}

	// a file's ObjectID is content, never touched
	if got, _ := mod.db.findEntry(file.ID); got.ObjectID == nil || *got.ObjectID != contentID("x") {
		t.Errorf("file ObjectID changed: %v", got.ObjectID)
	}
}

func TestClearFolderIDsNeverClearsAFile(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	file := addFile(t, mod, vol, vol.RootID, "f", "x")

	if _, err := mod.folderObjectID(vol.Identity, vol.RootID); err != nil {
		t.Fatalf("folderObjectID: %v", err)
	}
	if err := mod.db.clearFolderIDs(file.ID, 0); err != nil {
		t.Fatalf("clearFolderIDs: %v", err)
	}

	if got, _ := mod.db.findEntry(file.ID); got.ObjectID == nil || *got.ObjectID != contentID("x") {
		t.Errorf("file ObjectID = %v, want its content", got.ObjectID)
	}
	if got, _ := mod.db.findEntry(vol.RootID); got.ObjectID != nil {
		t.Error("the root above the file kept its cached ObjectID")
	}
}

func TestClearFolderIDsOnTheRootEndsAtTheRoot(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	if _, err := mod.folderObjectID(vol.Identity, vol.RootID); err != nil {
		t.Fatalf("folderObjectID: %v", err)
	}
	if err := mod.db.clearFolderIDs(vol.RootID, 0); err != nil {
		t.Fatalf("clearFolderIDs: %v", err)
	}
	if got, _ := mod.db.findEntry(vol.RootID); got.ObjectID != nil {
		t.Fatal("the root kept its cached ObjectID")
	}
}

func TestRootObjectIDFollowsEntryModTimeNotRootModTime(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	f := addFile(t, mod, vol, vol.RootID, "a", "x")
	before, _ := mod.folderObjectID(vol.Identity, vol.RootID)

	if err := mod.db.Model(&dbEntry{}).Where("id = ?", f.ID).Update("mod_time", testModTime+1).Error; err != nil {
		t.Fatal(err)
	}
	if err := mod.db.clearFolderIDs(vol.RootID, 0); err != nil {
		t.Fatal(err)
	}

	after, _ := mod.folderObjectID(vol.Identity, vol.RootID)
	if before == after {
		t.Fatal("a changed ModTime left the root ObjectID alone")
	}

	// the root's own time is outside the hashed bytes
	if err := mod.db.clearFolderIDs(vol.RootID, 0); err != nil {
		t.Fatal(err)
	}
	if err := mod.db.Model(&dbEntry{}).Where("id = ?", vol.RootID).Update("mod_time", testModTime+99).Error; err != nil {
		t.Fatal(err)
	}
	again, _ := mod.folderObjectID(vol.Identity, vol.RootID)
	if again != after {
		t.Fatal("the root's own ModTime changed the root ObjectID")
	}
}

func TestUsageIgnoresCachedFolderIDs(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	addFile(t, mod, vol, vol.RootID, "a", "hello")
	addFolder(t, mod, vol, vol.RootID, "dir")

	if _, err := mod.folderObjectID(vol.Identity, vol.RootID); err != nil {
		t.Fatalf("folderObjectID: %v", err)
	}

	// the root and the folder now carry directory IDs; neither is content
	if got, want := usageOf(t, mod, vol.Identity), uint64(5); got != want {
		t.Fatalf("usage = %d, want %d", got, want)
	}
}
