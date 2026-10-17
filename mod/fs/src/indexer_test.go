package fs

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// TestIndexerInitKeepsUnchangedFilesListed: right after init, before any worker runs, a watch
// repository scan lists every unchanged file, and every row the startup scan did not validate is
// queued for a check.
// note: indexing snapshots a watch repository at node start and records every absent object as removed.
func TestIndexerInitKeepsUnchangedFilesListed(t *testing.T) {
	root := t.TempDir()
	repo := testWatchRepository(t, root)
	repo.mod.log = &log.Logger{}
	repo.mod.indexer = NewIndexer(repo.mod)
	if err := repo.mod.indexer.addRoot(root); err != nil {
		t.Fatalf("addRoot: %v", err)
	}

	kept := filepath.Join(root, "kept.mp3")
	writeFile(t, kept, []byte("kept"))
	keptID := resolveString(t, "kept")
	stat, err := os.Stat(kept)
	if err != nil {
		t.Fatal(err)
	}
	indexPath(t, repo.mod.db, kept, keptID, stat.ModTime().UnixNano())

	changed := filepath.Join(root, "changed.mp3")
	writeFile(t, changed, []byte("new"))
	changedID := resolveString(t, "old")
	indexPath(t, repo.mod.db, changed, changedID, 1)

	gone := filepath.Join(root, "gone.mp3")
	goneID := resolveString(t, "gone")
	indexPath(t, repo.mod.db, gone, goneID, 1)

	// why: a clock set back since the last run leaves a row checked after the startup scan began.
	future := filepath.Join(root, "future.mp3")
	futureID := resolveString(t, "future")
	indexPath(t, repo.mod.db, future, futureID, 1)
	err = repo.mod.db.Model(&dbLocalFile{}).Where("path = ?", future).UpdateColumn("updated_at", int64(1)<<62).Error
	if err != nil {
		t.Fatalf("date row ahead: %v", err)
	}

	ctx := astral.NewContext(nil)
	if err := repo.mod.indexer.init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	listed := scanIDs(t, ctx, repo)
	if !slices.ContainsFunc(listed, keptID.IsEqual) {
		t.Errorf("scan = %v, want the unchanged file %v", listed, keptID)
	}
	for _, id := range []*astral.ObjectID{changedID, goneID, futureID} {
		if slices.ContainsFunc(listed, id.IsEqual) {
			t.Errorf("scan = %v, want no unchecked row %v", listed, id)
		}
	}

	queued := drainWorkqueue(repo.mod.indexer)
	want := []string{changed, future, gone}
	slices.Sort(want)
	if !slices.Equal(queued, want) {
		t.Errorf("queued = %v, want %v", queued, want)
	}

	for _, path := range queued {
		if err := repo.mod.indexer.checkAndFix(ctx, path); err != nil {
			t.Fatalf("checkAndFix(%v): %v", path, err)
		}
	}

	listed = scanIDs(t, ctx, repo)
	newID := resolveString(t, "new")
	if len(listed) != 2 || !slices.ContainsFunc(listed, keptID.IsEqual) || !slices.ContainsFunc(listed, newID.IsEqual) {
		t.Errorf("scan after checks = %v, want %v and %v", listed, keptID, newID)
	}
}

// scanIDs returns every object ID a scan of repo lists, without following.
func scanIDs(t *testing.T, ctx *astral.Context, repo *WatchRepository) []*astral.ObjectID {
	t.Helper()

	ch, err := repo.Scan(ctx, false)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var ids []*astral.ObjectID
	for id := range ch {
		ids = append(ids, id)
	}
	return ids
}

// drainWorkqueue returns the sorted paths waiting in the indexer's work queue.
func drainWorkqueue(indexer *Indexer) []string {
	var queued []string
	for {
		select {
		case path := <-indexer.workqueue:
			queued = append(queued, path)
		default:
			slices.Sort(queued)
			return queued
		}
	}
}
