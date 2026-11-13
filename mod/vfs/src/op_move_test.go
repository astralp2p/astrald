package vfs

import (
	"testing"
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
)

func TestMoveKeepsTheEntryTimeAndSetsBothParents(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	src := addFolder(t, mod, vol, vol.RootID, "src")
	addFile(t, mod, vol, src.ID, "f", "content")
	if err := mod.db.setEntryTime(vol.RootID, testModTime); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UnixNano()

	entry := changeTestEntry(t, changeTestCall(t, mod.OpMove, vol, map[string]any{"from": "/src/f", "to": "/dst/sub/g"}))

	if string(entry.Name) != "g" || entry.ID != contentID("content") || entry.ModTime.Time().UnixNano() != testModTime {
		t.Errorf("entry = %+v; want g with the content and the old time", entry)
	}
	if changeTestFind(t, mod, vol, "/src/f") != nil {
		t.Error("the entry is still at its old path")
	}
	for _, path := range []string{"/src", "/dst/sub", "/dst", "/"} {
		if got := changeTestMustFind(t, mod, vol, path); got.ModTime < start {
			t.Errorf("%s has time %d, want the node's time", path, got.ModTime)
		}
	}
	if changeTestMustFind(t, mod, vol, "/dst/sub/g").ModTime != testModTime {
		t.Error("the stored entry lost its time")
	}
}

func TestMoveAFolderAnswersItsListingID(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	dir := addFolder(t, mod, vol, vol.RootID, "dir")
	addFile(t, mod, vol, dir.ID, "f", "x")
	other := addFolder(t, mod, vol, vol.RootID, "other")

	if _, err := mod.folderObjectID(vol.Identity, vol.RootID); err != nil {
		t.Fatal(err)
	}

	entry := changeTestEntry(t, changeTestCall(t, mod.OpMove, vol, map[string]any{"from": "/dir", "to": "/other/dir"}))

	want, err := mod.folderObjectID(vol.Identity, dir.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !entry.IsFolder() || entry.ID != want {
		t.Errorf("entry = %+v, want the folder with ID %v", entry, want)
	}
	for _, row := range []int64{vol.RootID, other.ID} {
		got, _ := mod.db.findEntry(row)
		if got.ObjectID != nil {
			t.Errorf("row %d kept its cached ID", row)
		}
	}

	changeTestCheckRecompute(t, mod, vol)
}

func TestMoveRefusals(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	dir := addFolder(t, mod, vol, vol.RootID, "dir")
	addFolder(t, mod, vol, dir.ID, "sub")
	addFile(t, mod, vol, vol.RootID, "f", "x")

	for _, c := range []struct {
		from, to string
		want     error
	}{
		{"/dir", "/dir", vfsapi.ErrInvalidPath},
		{"/dir", "/dir/sub/x", vfsapi.ErrInvalidPath},
		{"/", "/x", vfsapi.ErrInvalidPath},
		{"/f", "/", vfsapi.ErrInvalidPath},
		{"/f", "/dir/sub", vfsapi.ErrAlreadyExists},
		{"/f", "/f", vfsapi.ErrAlreadyExists},
		{"/missing", "/x", vfsapi.ErrNotFound},
		{"/f", "/f/x", vfsapi.ErrNotAFolder},
		{"/f", "x", vfsapi.ErrInvalidPath},
	} {
		answer := changeTestCall(t, mod.OpMove, vol, map[string]any{"from": c.from, "to": c.to})
		changeTestErr(t, answer, c.want)
	}

	vol = changeTestSetLimits(t, mod, vol, 0, 3)
	answer := changeTestCall(t, mod.OpMove, vol, map[string]any{"from": "/f", "to": "/new/f"})
	changeTestErr(t, answer, vfsapi.ErrTooManyEntries)

	if changeTestFind(t, mod, vol, "/f") == nil || changeTestCount(t, mod, vol) != 3 {
		t.Error("a refused move changed the VFS")
	}
}
