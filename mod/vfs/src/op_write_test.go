package vfs

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

func TestWriteCreatesTheFileAndItsParents(t *testing.T) {
	mod, vol, repo := changeTestModule(t)
	if err := mod.db.changeSetTime(vol.RootID, testModTime); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UnixNano()

	entry := changeTestEntry(t, changeTestWrite(t, mod, vol, "/a/b/c.txt", "hello\n"))

	if string(entry.Name) != "c.txt" || entry.ID != contentID("hello\n") || entry.IsFolder() {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.ModTime.Time().UnixNano() < start {
		t.Errorf("the new entry has time %v, want the node's time", entry.ModTime)
	}
	if _, committed, _ := repo.counts(); committed != 1 {
		t.Errorf("%d objects committed, want 1", committed)
	}

	for _, path := range []string{"/", "/a", "/a/b"} {
		row := changeTestMustFind(t, mod, vol, path)
		if !row.isFolder() {
			t.Errorf("%s is not a folder", path)
		}
		if row.ModTime < start {
			t.Errorf("%s has time %d, want the node's time", path, row.ModTime)
		}
	}

	if n := changeTestCount(t, mod, vol); n != 3 {
		t.Errorf("%d entries, want 3", n)
	}
}

func TestWriteReplacesContentAndKeepsTheFolderTime(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	dir := addFolder(t, mod, vol, vol.RootID, "d")
	addFile(t, mod, vol, dir.ID, "f", "old")
	start := time.Now().UnixNano()

	entry := changeTestEntry(t, changeTestWrite(t, mod, vol, "/d/f", "new content"))

	if entry.ID != contentID("new content") {
		t.Errorf("ID = %v, want the new content", entry.ID)
	}
	if entry.ModTime.Time().UnixNano() < start {
		t.Errorf("the replaced file kept its time")
	}
	if got := changeTestMustFind(t, mod, vol, "/d"); got.ModTime != testModTime {
		t.Errorf("the folder time changed to %d", got.ModTime)
	}
	if n := changeTestCount(t, mod, vol); n != 2 {
		t.Errorf("%d entries, want 2", n)
	}
}

func TestWriteChecksTheTargetBeforeTheWriterOpens(t *testing.T) {
	mod, vol, repo := changeTestModule(t)
	addFolder(t, mod, vol, vol.RootID, "d")
	addFile(t, mod, vol, vol.RootID, "f", "x")

	for path, want := range map[string]error{
		"/":    vfsapi.ErrIsAFolder,
		"/d":   vfsapi.ErrIsAFolder,
		"/f/x": vfsapi.ErrNotAFolder,
		"/f/":  vfsapi.ErrInvalidPath,
	} {
		changeTestErr(t, changeTestCall(t, mod.OpWrite, vol, map[string]any{"path": path}), want)
	}

	if created, _, _ := repo.counts(); created != 0 {
		t.Errorf("%d writers opened, want 0", created)
	}
}

func TestWriteRefusesAStrangerBeforeTheWriterOpens(t *testing.T) {
	mod, vol, repo := changeTestModule(t)
	mod.Auth = newFakeAuth()

	c := changeTestRoute(t, mod.OpWrite, astral.GenerateIdentity(), map[string]any{"path": "/f", "vfs": vol.Identity.String()})
	changeTestErr(t, c.receive(), vfsapi.ErrNotPermitted)

	if created, _, _ := repo.counts(); created != 0 {
		t.Errorf("%d writers opened, want 0", created)
	}
}

func TestWriteInterruptedLeavesNoEntry(t *testing.T) {
	mod, vol, repo := changeTestModule(t)

	c := changeTestRoute(t, mod.OpWrite, vol.Identity, map[string]any{"path": "/a/f"})
	changeTestAck(t, c.receive())

	blob := astral.Blob("partial")
	<-c.send(&blob)
	c.in.Close()

	if answer := c.receive(); answer != nil {
		t.Fatalf("answer = %#v, want the stream closed", answer)
	}

	if changeTestFind(t, mod, vol, "/a") != nil || changeTestCount(t, mod, vol) != 0 {
		t.Error("an interrupted write left an entry")
	}
	if _, committed, discarded := repo.counts(); committed != 0 || discarded != 1 {
		t.Errorf("committed %d, discarded %d; want 0 and 1", committed, discarded)
	}
}

func TestWriteCutsOffAStreamAboveTheQuota(t *testing.T) {
	mod, vol, repo := changeTestModule(t)
	vol = changeTestSetLimits(t, mod, vol, 10, 0)

	changeTestErr(t, changeTestWrite(t, mod, vol, "/f", "0123456789x"), vfsapi.ErrQuotaExceeded)

	if changeTestFind(t, mod, vol, "/f") != nil {
		t.Error("a cut-off write left an entry")
	}
	if _, committed, discarded := repo.counts(); committed != 0 || discarded != 1 {
		t.Errorf("committed %d, discarded %d; want 0 and 1", committed, discarded)
	}

	// note: the size of the file being replaced counts towards the allowance.
	addFile(t, mod, vol, vol.RootID, "g", "0123456789")
	changeTestEntry(t, changeTestWrite(t, mod, vol, "/g", "abcdefghij"))
}

func TestWriteChecksTheQuotaExactlyAfterTheCommit(t *testing.T) {
	mod, vol, repo := changeTestModule(t)
	vol = changeTestSetLimits(t, mod, vol, 10, 0)
	addFile(t, mod, vol, vol.RootID, "a", "12345")
	addFile(t, mod, vol, vol.RootID, "b", "12345")

	// note: the stream fits the allowance (10 - 5 + 5), but b still holds the old content: usage would be 14.
	changeTestErr(t, changeTestWrite(t, mod, vol, "/a", "123456789"), vfsapi.ErrQuotaExceeded)

	if got := changeTestMustFind(t, mod, vol, "/a"); *got.ObjectID != contentID("12345") {
		t.Error("a refused write replaced the content")
	}
	if _, committed, _ := repo.counts(); committed != 1 {
		t.Errorf("%d objects committed, want the refused content left unheld in the repository", committed)
	}

	// note: the same content again counts once.
	changeTestEntry(t, changeTestWrite(t, mod, vol, "/c", "12345"))
}

func TestWriteRefusesTooManyEntries(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	vol = changeTestSetLimits(t, mod, vol, 0, 2)

	changeTestErr(t, changeTestWrite(t, mod, vol, "/x/y/z", "z"), vfsapi.ErrTooManyEntries)

	if n := changeTestCount(t, mod, vol); n != 0 {
		t.Errorf("%d entries after a refused write, want 0", n)
	}

	changeTestEntry(t, changeTestWrite(t, mod, vol, "/x/y", "y"))
}

func TestWriteAnEmptyFileCommitsNothing(t *testing.T) {
	mod, vol, repo := changeTestModule(t)

	entry := changeTestEntry(t, changeTestWrite(t, mod, vol, "/empty", ""))

	empty := astral.ObjectID{Hash: sha256.Sum256(nil)}
	if entry.ID != empty {
		t.Errorf("ID = %v, want the ObjectID of empty content", entry.ID)
	}
	if _, committed, discarded := repo.counts(); committed != 0 || discarded != 1 {
		t.Errorf("committed %d, discarded %d; want 0 and 1", committed, discarded)
	}
	if row := changeTestMustFind(t, mod, vol, "/empty"); *row.ObjectID != empty {
		t.Errorf("stored ID = %v", row.ObjectID)
	}
}

func TestWriteWithoutStorage(t *testing.T) {
	mod, vol, repo := changeTestModule(t)
	repo.createErr = errors.New("disk gone")

	changeTestErr(t, changeTestCall(t, mod.OpWrite, vol, map[string]any{"path": "/f"}), vfsapi.ErrNoStorage)

	mod.Objects = changeTestObjects{}
	changeTestErr(t, changeTestCall(t, mod.OpWrite, vol, map[string]any{"path": "/f"}), vfsapi.ErrNoStorage)
}

func TestWriteWaitsForTheLockOfTheVFS(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	unlock := mod.lockVFS(vol.Identity)

	answered := make(chan astral.Object, 1)
	go func() { answered <- changeTestWrite(t, mod, vol, "/f", "x") }()

	select {
	case answer := <-answered:
		t.Fatalf("answered %#v while another change held the lock", answer)
	case <-time.After(100 * time.Millisecond):
	}

	unlock()
	changeTestEntry(t, <-answered)
}

func TestConcurrentWritesToOneVFS(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	const n = 16

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := fmt.Sprintf("/d/sub/f%02d", i)
			if _, ok := changeTestWrite(t, mod, vol, path, path).(*vfsapi.Entry); !ok {
				errs <- fmt.Errorf("write %s failed", path)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	children, err := mod.db.listChildren(changeTestMustFind(t, mod, vol, "/d/sub").ID)
	if err != nil || len(children) != n {
		t.Fatalf("%d files (%v), want %d", len(children), err, n)
	}
	if got := changeTestCount(t, mod, vol); got != n+2 {
		t.Errorf("%d entries, want %d", got, n+2)
	}
}

func TestWriteClearsFolderIDsUpThePath(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	a := addFolder(t, mod, vol, vol.RootID, "a")
	addFolder(t, mod, vol, a.ID, "b")
	other := addFolder(t, mod, vol, vol.RootID, "other")
	addFile(t, mod, vol, other.ID, "o", "o")

	if _, err := mod.folderObjectID(vol.Identity, vol.RootID); err != nil {
		t.Fatal(err)
	}

	entry := changeTestEntry(t, changeTestWrite(t, mod, vol, "/a/b/f", "content"))

	for _, path := range []string{"/", "/a", "/a/b"} {
		if changeTestMustFind(t, mod, vol, path).ObjectID != nil {
			t.Errorf("%s kept its cached ID", path)
		}
	}
	if changeTestMustFind(t, mod, vol, "/other").ObjectID == nil {
		t.Error("a folder off the path lost its cached ID")
	}

	changeTestCheckRecompute(t, mod, vol)

	// note: the listing of b holds the one new file.
	listing, _ := vfsapi.NewListingWriter(nil, 1)
	if err := listing.Write(entry); err != nil {
		t.Fatal(err)
	}
	want, _ := listing.Close()
	if got := changeTestMustFind(t, mod, vol, "/a/b").ObjectID; got == nil || *got != *want {
		t.Errorf("ID of /a/b = %v, want %v", got, want)
	}
}

// changeTestCheckRecompute computes the root ObjectID from the cache, then again with every cache cleared, and
// fails when the two differ.
func changeTestCheckRecompute(t *testing.T, mod *Module, vol *dbVolume) {
	t.Helper()

	cached, err := mod.folderObjectID(vol.Identity, vol.RootID)
	if err != nil {
		t.Fatal(err)
	}

	if err = mod.db.Model(&dbEntry{}).Where("vfs = ? AND flags & 1 = 1", vol.Identity).
		Update("object_id", nil).Error; err != nil {
		t.Fatal(err)
	}

	fresh, err := mod.folderObjectID(vol.Identity, vol.RootID)
	if err != nil {
		t.Fatal(err)
	}
	if cached != fresh {
		t.Errorf("root ID from the cache %v, from scratch %v", cached, fresh)
	}
}
