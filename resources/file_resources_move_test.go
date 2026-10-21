package resources

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

const moveName = "node_key"

// newMoveRoots returns FileResources with a config root and a separate data root, neither holding moveName.
func newMoveRoots(t *testing.T) (res *FileResources, cfg, data string) {
	t.Helper()
	tmp := t.TempDir()
	cfg, data = filepath.Join(tmp, "config"), filepath.Join(tmp, "data")

	res, err := NewFileResources(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	res.SetDataRoot(data)

	return res, cfg, data
}

func writeFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, moveName), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// wantFile fails unless dir holds moveName with content; an empty content means no file.
func wantFile(t *testing.T, dir, content string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, moveName))
	switch {
	case content == "" && !errors.Is(err, fs.ErrNotExist):
		t.Fatalf("%s/%s: read = (%q, %v), want no file", dir, moveName, got, err)
	case content != "" && (err != nil || string(got) != content):
		t.Fatalf("%s/%s: read = (%q, %v), want %q", dir, moveName, got, err, content)
	}
}

// skipAsRoot skips a test that relies on directory permissions, which root ignores.
func skipAsRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
}

func TestMoveToDataRootBothAbsent(t *testing.T) {
	res, cfg, data := newMoveRoots(t)

	if err := res.MoveToDataRoot(moveName); err != nil {
		t.Fatalf("MoveToDataRoot = %v, want nil", err)
	}
	wantFile(t, cfg, "")
	wantFile(t, data, "")
}

func TestMoveToDataRootConfigOnlyMoves(t *testing.T) {
	res, cfg, data := newMoveRoots(t)
	writeFile(t, cfg, "key")

	if err := res.MoveToDataRoot(moveName); err != nil {
		t.Fatalf("MoveToDataRoot = %v, want nil", err)
	}
	wantFile(t, cfg, "")
	wantFile(t, data, "key")

	fi, err := os.Stat(filepath.Join(data, moveName))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("moved file mode = %o, want 600", got)
	}
}

func TestMoveToDataRootDataOnlyUntouched(t *testing.T) {
	res, cfg, data := newMoveRoots(t)
	writeFile(t, data, "key")

	if err := res.MoveToDataRoot(moveName); err != nil {
		t.Fatalf("MoveToDataRoot = %v, want nil", err)
	}
	wantFile(t, cfg, "")
	wantFile(t, data, "key")
}

// TestMoveToDataRootBothEqualRemovesConfigCopy pins the completion of a move
// whose removal failed on an earlier start.
func TestMoveToDataRootBothEqualRemovesConfigCopy(t *testing.T) {
	res, cfg, data := newMoveRoots(t)
	writeFile(t, cfg, "key")
	writeFile(t, data, "key")

	if err := res.MoveToDataRoot(moveName); err != nil {
		t.Fatalf("MoveToDataRoot = %v, want nil", err)
	}
	wantFile(t, cfg, "")
	wantFile(t, data, "key")
}

// TestMoveToDataRootBothDifferKeepsBoth: a config copy that differs from the
// data-root copy was never copied, so it is reported and never removed.
func TestMoveToDataRootBothDifferKeepsBoth(t *testing.T) {
	res, cfg, data := newMoveRoots(t)
	writeFile(t, cfg, "old")
	writeFile(t, data, "new")

	if err := res.MoveToDataRoot(moveName); !errors.Is(err, ErrRootCopyKept) {
		t.Fatalf("MoveToDataRoot = %v, want ErrRootCopyKept", err)
	}
	wantFile(t, cfg, "old")
	wantFile(t, data, "new")
}

// TestMoveToDataRootReadOnlyConfigKeepsCopy: a config root that refuses the
// removal still gets the data-root copy, and a later start with a writable
// config root completes the move.
func TestMoveToDataRootReadOnlyConfigKeepsCopy(t *testing.T) {
	skipAsRoot(t)
	res, cfg, data := newMoveRoots(t)
	writeFile(t, cfg, "key")
	if err := os.Chmod(cfg, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(cfg, 0700) })

	for i := 0; i < 2; i++ {
		if err := res.MoveToDataRoot(moveName); !errors.Is(err, ErrRootCopyKept) {
			t.Fatalf("MoveToDataRoot #%d on a read-only config root = %v, want ErrRootCopyKept", i+1, err)
		}
		wantFile(t, cfg, "key")
		wantFile(t, data, "key")
	}

	if err := os.Chmod(cfg, 0700); err != nil {
		t.Fatal(err)
	}
	if err := res.MoveToDataRoot(moveName); err != nil {
		t.Fatalf("MoveToDataRoot on a writable config root = %v, want nil", err)
	}
	wantFile(t, cfg, "")
	wantFile(t, data, "key")
}

// TestMoveToDataRootUnreadableConfigCopy: a config copy that cannot be read is
// an error, and nothing is written to the data root.
func TestMoveToDataRootUnreadableConfigCopy(t *testing.T) {
	res, cfg, data := newMoveRoots(t)
	if err := os.Mkdir(filepath.Join(cfg, moveName), 0700); err != nil {
		t.Fatal(err)
	}

	err := res.MoveToDataRoot(moveName)
	if err == nil || errors.Is(err, ErrRootCopyKept) {
		t.Fatalf("MoveToDataRoot = %v, want an error other than ErrRootCopyKept", err)
	}
	if fi, err := os.Stat(filepath.Join(cfg, moveName)); err != nil || !fi.IsDir() {
		t.Fatalf("config copy changed: %v", err)
	}
	wantFile(t, data, "")
}

// TestMoveToDataRootWriteFailureKeepsConfigCopy: a data root that refuses the
// write is an error, and the config copy stays.
func TestMoveToDataRootWriteFailureKeepsConfigCopy(t *testing.T) {
	skipAsRoot(t)
	res, cfg, data := newMoveRoots(t)
	writeFile(t, cfg, "key")
	if err := os.Chmod(data, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(data, 0700) })

	err := res.MoveToDataRoot(moveName)
	if err == nil || errors.Is(err, ErrRootCopyKept) {
		t.Fatalf("MoveToDataRoot = %v, want an error other than ErrRootCopyKept", err)
	}
	wantFile(t, cfg, "key")
	wantFile(t, data, "")
}

// TestMoveToDataRootDataStatError: a data root that cannot be examined is an
// error, and the config copy stays.
func TestMoveToDataRootDataStatError(t *testing.T) {
	res, cfg, _ := newMoveRoots(t)
	writeFile(t, cfg, "key")
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	res.SetDataRoot(file)

	err := res.MoveToDataRoot(moveName)
	if err == nil || errors.Is(err, ErrRootCopyKept) {
		t.Fatalf("MoveToDataRoot = %v, want an error other than ErrRootCopyKept", err)
	}
	wantFile(t, cfg, "key")
}

// TestMoveToDataRootSameDirectory: with no separate data root both paths are
// one file, which stays.
func TestMoveToDataRootSameDirectory(t *testing.T) {
	cfg := t.TempDir()
	res, err := NewFileResources(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, cfg, "key")

	if err := res.MoveToDataRoot(moveName); err != nil {
		t.Fatalf("MoveToDataRoot = %v, want nil", err)
	}
	wantFile(t, cfg, "key")
}

// TestMoveToDataRootLinkedDataRoot: a data root that links to the config root
// makes both paths one file, which stays.
func TestMoveToDataRootLinkedDataRoot(t *testing.T) {
	res, cfg, _ := newMoveRoots(t)
	writeFile(t, cfg, "key")
	link := filepath.Join(t.TempDir(), "data")
	if err := os.Symlink(cfg, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	res.SetDataRoot(link)

	if err := res.MoveToDataRoot(moveName); err != nil {
		t.Fatalf("MoveToDataRoot = %v, want nil", err)
	}
	wantFile(t, cfg, "key")
}
