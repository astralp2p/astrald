package vfs

import (
	"testing"
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
)

func TestMkdirCreatesTheFolderAndItsParents(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	if err := mod.db.setEntryTime(vol.RootID, testModTime); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UnixNano()

	changeTestAck(t, changeTestCall(t, mod.OpMkdir, vol, map[string]any{"path": "/a/b/c"}))

	for _, path := range []string{"/", "/a", "/a/b", "/a/b/c"} {
		row := changeTestMustFind(t, mod, vol, path)
		if !row.isFolder() || row.ModTime < start {
			t.Errorf("%s: folder %v, time %d; want a folder with the node's time", path, row.isFolder(), row.ModTime)
		}
	}
	if n := changeTestCount(t, mod, vol); n != 3 {
		t.Errorf("%d entries, want 3", n)
	}

	// note: an add sets one node time on the new entry and on its parent.
	if c, b := changeTestMustFind(t, mod, vol, "/a/b/c"), changeTestMustFind(t, mod, vol, "/a/b"); c.ModTime != b.ModTime {
		t.Errorf("/a/b/c has time %d and /a/b %d, want one time", c.ModTime, b.ModTime)
	}
}

func TestMkdirOnAnExistingFolderSucceeds(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	addFolder(t, mod, vol, vol.RootID, "d")

	changeTestAck(t, changeTestCall(t, mod.OpMkdir, vol, map[string]any{"path": "/d"}))
	changeTestAck(t, changeTestCall(t, mod.OpMkdir, vol, map[string]any{"path": "/"}))

	if got := changeTestMustFind(t, mod, vol, "/d"); got.ModTime != testModTime {
		t.Errorf("an existing folder changed its time to %d", got.ModTime)
	}
	if n := changeTestCount(t, mod, vol); n != 1 {
		t.Errorf("%d entries, want 1", n)
	}
}

func TestMkdirRefusals(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	addFile(t, mod, vol, vol.RootID, "f", "x")

	changeTestErr(t, changeTestCall(t, mod.OpMkdir, vol, map[string]any{"path": "/f"}), vfsapi.ErrAlreadyExists)
	changeTestErr(t, changeTestCall(t, mod.OpMkdir, vol, map[string]any{"path": "/f/sub"}), vfsapi.ErrNotAFolder)
	changeTestErr(t, changeTestCall(t, mod.OpMkdir, vol, map[string]any{"path": "a"}), vfsapi.ErrInvalidPath)

	vol = changeTestSetLimits(t, mod, vol, 0, 2)
	changeTestErr(t, changeTestCall(t, mod.OpMkdir, vol, map[string]any{"path": "/x/y"}), vfsapi.ErrNoInodesLeft)

	if n := changeTestCount(t, mod, vol); n != 1 {
		t.Errorf("%d entries after the refusals, want 1", n)
	}
}

// TestMkdirOfTheRootChangesNothing: mkdir of the root answers ack and leaves the root's time, the root's cached ID
// and the entry count as they were.
func TestMkdirOfTheRootChangesNothing(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	addFolder(t, mod, vol, vol.RootID, "d")
	changeTestSetTime(t, mod, vol.RootID, testModTime)
	root, err := mod.folderObjectID(vol.Identity, vol.RootID)
	if err != nil {
		t.Fatal(err)
	}

	changeTestAck(t, changeTestCall(t, mod.OpMkdir, vol, map[string]any{"path": "/"}))

	got := changeTestMustFind(t, mod, vol, "/")
	if got.ModTime != testModTime {
		t.Errorf("root time %d, want %d", got.ModTime, testModTime)
	}
	if got.ObjectID == nil || *got.ObjectID != root {
		t.Errorf("root cached ID %v, want %v", got.ObjectID, root)
	}
	if n := changeTestCount(t, mod, vol); n != 1 {
		t.Errorf("%d entries, want 1", n)
	}
}
