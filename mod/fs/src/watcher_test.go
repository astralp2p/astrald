package fs

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

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
