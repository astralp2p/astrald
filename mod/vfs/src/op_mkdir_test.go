package vfs

import (
	"testing"
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
)

func TestMkdirCreatesTheFolderAndItsParents(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	if err := mod.db.changeSetTime(vol.RootID, testModTime); err != nil {
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
	changeTestErr(t, changeTestCall(t, mod.OpMkdir, vol, map[string]any{"path": "/x/y"}), vfsapi.ErrTooManyEntries)

	if n := changeTestCount(t, mod, vol); n != 1 {
		t.Errorf("%d entries after the refusals, want 1", n)
	}
}
