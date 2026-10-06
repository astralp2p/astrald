package paths

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func newWalkTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "3.txt"))
	writeTestFile(t, filepath.Join(root, "a", "1.txt"))
	writeTestFile(t, filepath.Join(root, "a", "b", "2.txt"))
	if err := os.Symlink(filepath.Join(root, "3.txt"), filepath.Join(root, "link")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	return root
}

func collectWalk(t *testing.T, root string) []string {
	t.Helper()
	var got []string
	err := WalkDir(context.Background(), root, func(path string, info os.FileInfo) error {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		got = append(got, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	return got
}

func TestWalkDirReportsRegularFilesBreadthFirst(t *testing.T) {
	root := newWalkTree(t)

	got := collectWalk(t, root)

	want := []string{"3.txt", filepath.Join("a", "1.txt"), filepath.Join("a", "b", "2.txt")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WalkDir reported %q, want %q", got, want)
	}
}

func TestWalkDirStopsOnCallbackError(t *testing.T) {
	root := newWalkTree(t)
	errX := errors.New("x")

	calls := 0
	err := WalkDir(context.Background(), root, func(string, os.FileInfo) error {
		calls++
		return errX
	})

	if !errors.Is(err, errX) {
		t.Fatalf("WalkDir error = %v, want %v", err, errX)
	}
	if calls != 1 {
		t.Fatalf("fn called %d times, want 1", calls)
	}
}

func TestWalkDirCanceledContext(t *testing.T) {
	root := newWalkTree(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := WalkDir(ctx, root, func(string, os.FileInfo) error {
		calls++
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WalkDir error = %v, want %v", err, context.Canceled)
	}
	if calls != 0 {
		t.Fatalf("fn called %d times, want 0", calls)
	}
}

func TestWalkDirMissingRoot(t *testing.T) {
	calls := 0
	err := WalkDir(context.Background(), filepath.Join(t.TempDir(), "missing"), func(string, os.FileInfo) error {
		calls++
		return nil
	})

	if err != nil {
		t.Fatalf("WalkDir error = %v, want nil", err)
	}
	if calls != 0 {
		t.Fatalf("fn called %d times, want 0", calls)
	}
}

func TestWalkDirSkipsUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a mode 000 directory")
	}
	root := newWalkTree(t)
	locked := filepath.Join(root, "locked")
	writeTestFile(t, filepath.Join(locked, "hidden.txt"))
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700) })

	got := collectWalk(t, root)

	want := []string{"3.txt", filepath.Join("a", "1.txt"), filepath.Join("a", "b", "2.txt")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WalkDir reported %q, want %q", got, want)
	}
}
