package vfs

import (
	"testing"
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
)

// The rules of the creation time: ticket "VFS: add a creation time to the entry".

// creationTimeOf returns the creation time of an entry in nanoseconds since the Unix epoch.
func creationTimeOf(e *vfsapi.Entry) int64 { return e.CreationTime.Time().UnixNano() }

func TestCreationTimeWriteToANewPathSetsTheNodeTime(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	start := time.Now().UnixNano()

	entry := changeTestEntry(t, changeTestWrite(t, mod, vol, "/a/f", "x"))

	if got := creationTimeOf(entry); got < start || got != entry.ModTime.Time().UnixNano() {
		t.Errorf("answered creation time %d, want the node time %d of the add", got, entry.ModTime.Time().UnixNano())
	}
	if row := changeTestMustFind(t, mod, vol, "/a/f"); row.CreationTime != row.ModTime {
		t.Errorf("/a/f: creation time %d, mod time %d; want the node time of the add", row.CreationTime, row.ModTime)
	}
	// note: /a is made implicitly; its mod time moves on when /a/f is added, its creation time does not.
	if row := changeTestMustFind(t, mod, vol, "/a"); row.CreationTime < start || row.CreationTime > row.ModTime {
		t.Errorf("/a: creation time %d, mod time %d; want the node time of its add", row.CreationTime, row.ModTime)
	}
}

func TestCreationTimeMkdirSetsTheNodeTime(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	start := time.Now().UnixNano()

	changeTestAck(t, changeTestCall(t, mod.OpMkdir, vol, map[string]any{"path": "/a/b"}))

	for _, path := range []string{"/a", "/a/b"} {
		if row := changeTestMustFind(t, mod, vol, path); row.CreationTime < start {
			t.Errorf("%s: creation time %d, want the node time", path, row.CreationTime)
		}
	}
}

func TestCreationTimeWriteOverAFileKeepsIt(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	addFile(t, mod, vol, vol.RootID, "f", "old")

	entry := changeTestEntry(t, changeTestWrite(t, mod, vol, "/f", "new"))

	if got := creationTimeOf(entry); got != testCreationTime {
		t.Errorf("answered creation time %d, want %d", got, testCreationTime)
	}
	if got := changeTestMustFind(t, mod, vol, "/f").CreationTime; got != testCreationTime {
		t.Errorf("stored creation time %d, want %d", got, testCreationTime)
	}
}

func TestCreationTimeMoveKeepsIt(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	addFile(t, mod, vol, vol.RootID, "f", "x")
	addFolder(t, mod, vol, vol.RootID, "d")

	for _, c := range []struct{ from, to string }{{"/f", "/g"}, {"/g", "/elsewhere/g"}, {"/d", "/e"}} {
		entry := changeTestEntry(t, changeTestCall(t, mod.OpMove, vol, map[string]any{"from": c.from, "to": c.to}))
		if got := creationTimeOf(entry); got != testCreationTime {
			t.Errorf("move %s to %s: answered creation time %d, want %d", c.from, c.to, got, testCreationTime)
		}
		if got := changeTestMustFind(t, mod, vol, c.to).CreationTime; got != testCreationTime {
			t.Errorf("move %s to %s: stored creation time %d, want %d", c.from, c.to, got, testCreationTime)
		}
	}
}

func TestCreationTimeTouchKeepsIt(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	addFile(t, mod, vol, vol.RootID, "f", "x")
	when := time.Date(2030, 1, 2, 3, 4, 5, 6, time.UTC)

	entry := changeTestEntry(t, changeTestCall(t, mod.OpTouch, vol, map[string]any{"path": "/f", "time": when.Format(time.RFC3339Nano)}))

	if got := creationTimeOf(entry); got != testCreationTime {
		t.Errorf("answered creation time %d, want %d", got, testCreationTime)
	}
	if row := changeTestMustFind(t, mod, vol, "/f"); row.CreationTime != testCreationTime || row.ModTime != when.UnixNano() {
		t.Errorf("stored creation time %d, mod time %d; want %d and %d", row.CreationTime, row.ModTime, testCreationTime, when.UnixNano())
	}
}

func TestCreationTimeRemoveThenCreateGivesANewOne(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	addFile(t, mod, vol, vol.RootID, "f", "x")

	changeTestAck(t, changeTestCall(t, mod.OpRemove, vol, map[string]any{"path": "/f"}))
	start := time.Now().UnixNano()
	entry := changeTestEntry(t, changeTestWrite(t, mod, vol, "/f", "x"))

	if got := creationTimeOf(entry); got < start {
		t.Errorf("creation time %d after remove and write, want a new one", got)
	}
}

func TestCreationTimeTheRootHasNone(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	changeTestWrite(t, mod, vol, "/f", "x")

	if got := changeTestMustFind(t, mod, vol, "/").CreationTime; got != 0 {
		t.Errorf("root creation time %d, want 0", got)
	}
	if got := creationTimeOf(changeTestEntry(t, changeTestCall(t, mod.OpStat, vol, map[string]any{"path": "/"}))); got != 0 {
		t.Errorf("stat / creation time %d, want 0", got)
	}
}
