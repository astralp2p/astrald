package vfs

import (
	"testing"
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
)

func TestRemoveAFile(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	dir := addFolder(t, mod, vol, vol.RootID, "d")
	addFile(t, mod, vol, dir.ID, "f", "content")
	if n := changeTestHolders(t, mod, contentID("content")); n != 1 {
		t.Fatalf("%d entries hold the content before the removal, want 1", n)
	}
	start := time.Now().UnixNano()

	changeTestAck(t, changeTestCall(t, mod.OpRemove, vol, map[string]any{"path": "/d/f"}))

	if changeTestFind(t, mod, vol, "/d/f") != nil {
		t.Error("the file is still there")
	}
	if got := changeTestMustFind(t, mod, vol, "/d"); got.ModTime < start {
		t.Errorf("the folder has time %d, want the node's time", got.ModTime)
	}
	if n := changeTestHolders(t, mod, contentID("content")); n != 0 {
		t.Errorf("%d entries still hold the removed content", n)
	}
}

func TestRemoveAFolder(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	dir := addFolder(t, mod, vol, vol.RootID, "d")
	sub := addFolder(t, mod, vol, dir.ID, "sub")
	addFile(t, mod, vol, sub.ID, "f", "deep")
	addFolder(t, mod, vol, vol.RootID, "empty")
	addFile(t, mod, vol, vol.RootID, "keep", "keep")

	changeTestErr(t, changeTestCall(t, mod.OpRemove, vol, map[string]any{"path": "/d"}), vfsapi.ErrFolderNotEmpty)
	changeTestAck(t, changeTestCall(t, mod.OpRemove, vol, map[string]any{"path": "/empty"}))
	changeTestAck(t, changeTestCall(t, mod.OpRemove, vol, map[string]any{"path": "/d", "recursive": "true"}))

	if n := changeTestCount(t, mod, vol); n != 1 {
		t.Errorf("%d entries, want only /keep", n)
	}
	if n := changeTestHolders(t, mod, contentID("deep")); n != 0 {
		t.Errorf("%d entries still hold the removed content", n)
	}

	changeTestCheckRecompute(t, mod, vol)
}

func TestRemoveRefusals(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	addFile(t, mod, vol, vol.RootID, "f", "x")

	changeTestErr(t, changeTestCall(t, mod.OpRemove, vol, map[string]any{"path": "/"}), vfsapi.ErrInvalidPath)
	changeTestErr(t, changeTestCall(t, mod.OpRemove, vol, map[string]any{"path": "/", "recursive": "true"}), vfsapi.ErrInvalidPath)
	changeTestErr(t, changeTestCall(t, mod.OpRemove, vol, map[string]any{"path": "/missing"}), vfsapi.ErrNotFound)
	changeTestErr(t, changeTestCall(t, mod.OpRemove, vol, map[string]any{"path": "/f/x"}), vfsapi.ErrNotAFolder)

	if n := changeTestCount(t, mod, vol); n != 1 {
		t.Errorf("%d entries, want 1", n)
	}
}

// changeTestHolders returns the number of entries, in any VFS, whose ObjectID is id.
func changeTestHolders(t *testing.T, mod *Module, id interface{}) int64 {
	t.Helper()

	var n int64
	if err := mod.db.Model(&dbEntry{}).Where("object_id = ?", id).Count(&n).Error; err != nil {
		t.Fatal(err)
	}

	return n
}
