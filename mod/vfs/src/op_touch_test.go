package vfs

import (
	"testing"
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
)

func TestTouchSetsTheTimeOfFilesAndFolders(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	dir := addFolder(t, mod, vol, vol.RootID, "d")
	addFile(t, mod, vol, dir.ID, "f", "x")
	if err := mod.db.setEntryTime(vol.RootID, testModTime); err != nil {
		t.Fatal(err)
	}
	if _, err := mod.folderObjectID(vol.Identity, vol.RootID); err != nil {
		t.Fatal(err)
	}

	when := time.Date(2030, 1, 2, 3, 4, 5, 6, time.UTC)
	for _, path := range []string{"/d/f", "/d"} {
		answer := changeTestCall(t, mod.OpTouch, vol, map[string]any{"path": path, "time": when.Format(time.RFC3339Nano)})
		entry := changeTestEntry(t, answer)
		if entry.ModTime.Time().UnixNano() != when.UnixNano() {
			t.Errorf("%s: time %v, want %v", path, entry.ModTime, when)
		}
		if got := changeTestMustFind(t, mod, vol, path).ModTime; got != when.UnixNano() {
			t.Errorf("%s: stored time %d", path, got)
		}
	}

	if got := changeTestMustFind(t, mod, vol, "/"); got.ModTime != testModTime || got.ObjectID != nil {
		t.Errorf("root: time %d, cached ID %v; want the old time and a cleared ID", got.ModTime, got.ObjectID)
	}

	changeTestCheckRecompute(t, mod, vol)
}

func TestTouchDefaultsToTheNodeTime(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	addFile(t, mod, vol, vol.RootID, "f", "x")
	start := time.Now().UnixNano()

	entry := changeTestEntry(t, changeTestCall(t, mod.OpTouch, vol, map[string]any{"path": "/f"}))

	if entry.ModTime.Time().UnixNano() < start {
		t.Errorf("time %v, want the node's time", entry.ModTime)
	}
}

func TestTouchRefusals(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	addFile(t, mod, vol, vol.RootID, "f", "x")

	changeTestErr(t, changeTestCall(t, mod.OpTouch, vol, map[string]any{"path": "/"}), vfsapi.ErrInvalidPath)
	changeTestErr(t, changeTestCall(t, mod.OpTouch, vol, map[string]any{"path": "/missing"}), vfsapi.ErrNotFound)

	for _, when := range []string{"1960-01-01T00:00:00Z", "2300-01-01T00:00:00Z"} {
		answer := changeTestCall(t, mod.OpTouch, vol, map[string]any{"path": "/f", "time": when})
		changeTestErr(t, answer, vfsapi.ErrInvalidTime)
	}

	if changeTestFind(t, mod, vol, "/missing") != nil {
		t.Error("touch created an entry")
	}
	if got := changeTestMustFind(t, mod, vol, "/f").ModTime; got != testModTime {
		t.Errorf("a refused touch set the time to %d", got)
	}
}

// TestTouchAFolderKeepsItsOwnCachedIDAndClearsAncestors: a touched folder keeps its cached ID, because its time is
// outside its own directory, while the root loses its cached ID, and stat answers the ID the folder had before.
func TestTouchAFolderKeepsItsOwnCachedIDAndClearsAncestors(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	dir := addFolder(t, mod, vol, vol.RootID, "d")
	addFile(t, mod, vol, dir.ID, "f", "x")
	if _, err := mod.folderObjectID(vol.Identity, vol.RootID); err != nil {
		t.Fatal(err)
	}
	cached := changeTestMustFind(t, mod, vol, "/d").ObjectID
	if cached == nil {
		t.Fatal("/d has no cached ID")
	}
	want := changeTestEntry(t, changeTestCall(t, mod.OpStat, vol, map[string]any{"path": "/d"})).ID

	changeTestEntry(t, changeTestCall(t, mod.OpTouch, vol, map[string]any{"path": "/d"}))

	if got := changeTestMustFind(t, mod, vol, "/d").ObjectID; got == nil || *got != *cached {
		t.Errorf("/d: cached ID %v, want %v", got, *cached)
	}
	if got := changeTestMustFind(t, mod, vol, "/").ObjectID; got != nil {
		t.Errorf("/: cached ID %v, want a cleared ID", got)
	}
	if got := changeTestEntry(t, changeTestCall(t, mod.OpStat, vol, map[string]any{"path": "/d"})).ID; got != want {
		t.Errorf("stat /d: ID %v, want %v", got, want)
	}
}
