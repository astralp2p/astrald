package fs

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// newTimeoutWatcher builds a Watcher for the debounce tests alone.
//
// why no fsnotify instance: onWrite and onRemoved touch only mu, timeouts,
// WriteTimeout and OnWriteDone, so the watcher never reaches the kernel.
func newTimeoutWatcher(writeTimeout time.Duration, done func(string)) *Watcher {
	return &Watcher{
		WriteTimeout: writeTimeout,
		OnWriteDone:  done,
		timeouts:     map[string]*time.Time{},
	}
}

// TestWatcher_RemoveClearsPendingWriteTimeout: a path removed mid-debounce
// leaves no entry behind. No timing: the deadline is an hour out and never due.
func TestWatcher_RemoveClearsPendingWriteTimeout(t *testing.T) {
	w := newTimeoutWatcher(time.Hour, nil)

	w.onWrite("/x")
	w.mu.Lock()
	_, found := w.timeouts["/x"]
	w.mu.Unlock()
	if !found {
		t.Fatal("onWrite left no pending timeout to clear")
	}

	w.onRemoved("/x")

	w.mu.Lock()
	_, found = w.timeouts["/x"]
	w.mu.Unlock()
	if found {
		t.Error("onRemoved left the timeout entry behind; a later write will schedule no timer")
	}
}

// TestWatcher_RenameClearsPendingWriteTimeout: onRenamed delegates to
// onRemoved, so the rename path carries the same defect and the same fix.
func TestWatcher_RenameClearsPendingWriteTimeout(t *testing.T) {
	w := newTimeoutWatcher(time.Hour, nil)

	w.onWrite("/x")
	w.onRenamed("/x")

	w.mu.Lock()
	_, found := w.timeouts["/x"]
	w.mu.Unlock()
	if found {
		t.Error("onRenamed left the timeout entry behind; a later write will schedule no timer")
	}
}

// TestWatcher_AddTreeReturnsAddedPaths: a tree-mode Add reports the directories
// it watched, the regular files among them contributing nothing.
func TestWatcher_AddTreeReturnsAddedPaths(t *testing.T) {
	// why: the tree is built before NewWatcher, so no event reaches the worker
	// and the test observes Add's return alone.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	w, err := NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	added, err := w.Add(root, true)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	slices.Sort(added)
	want := []string{root, filepath.Join(root, "sub")}
	if !slices.Equal(added, want) {
		t.Errorf("Add(tree) returned %v; want %v", added, want)
	}
}
