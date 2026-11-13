package vfs

import (
	"encoding/hex"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// The vectors of VFS 6, section 5. Every ModTime is 1700000000000000000.
const (
	v1Hash = "bca43d0a568b64607f38b186a87810458f6776e86ae1978408b6e5790a00c242"
	v2Hash = "5ab8b70ba82974c48df86f4bcf3525cabe79931cff16d52d9fbb7c281f381eae"
	v3Hash = "44ef65c004d37cd130e83e85f7e994b0ddbf39f8fe072faedfaad32b5e87781b"
	v4Hash = "7a9882a18b111cf488f7c0de46fc95d4a1d8356c6adcc33b63d21550b6393049"
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

func TestVectorV1EmptyFolder(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	requireFolderID(t, mod, vol, vol.RootID, 24, v1Hash)
}

func TestVectorV2OneFile(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	addFile(t, mod, vol, vol.RootID, "a.txt", "hello\n")

	requireFolderID(t, mod, vol, vol.RootID, 80, v2Hash)
}

func TestVectorV3OneSubfolder(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	sub := addFolder(t, mod, vol, vol.RootID, "sub")

	requireFolderID(t, mod, vol, vol.RootID, 78, v3Hash)

	// the empty sub-folder is V1, and the computation cached it
	requireFolderID(t, mod, vol, sub.ID, 24, v1Hash)
}

func TestVectorV4ByteOrderOfNames(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())

	// inserted out of order on purpose
	addFile(t, mod, vol, vol.RootID, "é", "3")
	addFile(t, mod, vol, vol.RootID, "z", "1")
	addFile(t, mod, vol, vol.RootID, "è", "2")

	requireFolderID(t, mod, vol, vol.RootID, 182, v4Hash)
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

	id, filled, err := mod.db.computeFolderID(vol.RootID)
	if err != nil {
		t.Fatalf("computeFolderID: %v", err)
	}
	if len(filled) != 1 || filled[0].Row != vol.RootID {
		t.Fatalf("filled = %v, want the root only", filled)
	}

	if err := mod.db.clearFolderIDs(sub.ID); err != nil {
		t.Fatalf("clearFolderIDs: %v", err)
	}
	fresh, _, err := mod.db.computeFolderID(vol.RootID)
	if err != nil {
		t.Fatalf("computeFolderID: %v", err)
	}
	if fresh == id {
		t.Fatal("the listing did not change after the cache was cleared")
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

	if err := mod.db.clearFolderIDs(b.ID); err != nil {
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
	if err := mod.db.clearFolderIDs(file.ID); err != nil {
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
	if err := mod.db.clearFolderIDs(vol.RootID); err != nil {
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
	if err := mod.db.clearFolderIDs(vol.RootID); err != nil {
		t.Fatal(err)
	}

	after, _ := mod.folderObjectID(vol.Identity, vol.RootID)
	if before == after {
		t.Fatal("a changed ModTime left the root ObjectID alone")
	}

	// the root's own time is outside the hashed bytes
	if err := mod.db.clearFolderIDs(vol.RootID); err != nil {
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

	// the root and the folder now carry listing IDs; neither is content
	if got, want := usageOf(t, mod, vol.Identity), uint64(5); got != want {
		t.Fatalf("usage = %d, want %d", got, want)
	}
}
