package resources

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNewFileResourcesRejectsRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	res, err := NewFileResources(path, true)
	if want := "path is not a directory"; err == nil || err.Error() != want {
		t.Fatalf("NewFileResources(regular file) = (%v, %v), want error %q", res, err, want)
	}
}

func TestNewFileResourcesCreatesDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new")

	res, err := NewFileResources(path, true)
	if err != nil {
		t.Fatalf("NewFileResources: %v", err)
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		t.Fatalf("root %q not created as a directory: %v", path, err)
	}
	if got := res.Root(); got != path {
		t.Fatalf("Root() = %q, want %q", got, path)
	}
}

func TestFileResourcesReadMissing(t *testing.T) {
	res, err := NewFileResources(t.TempDir(), true)
	if err != nil {
		t.Fatalf("NewFileResources: %v", err)
	}

	if _, err := res.Read("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Read(missing) error = %v, want %v", err, ErrNotFound)
	}
}

func TestFileResourcesWriteRecreatesRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	res, err := NewFileResources(root, true)
	if err != nil {
		t.Fatalf("NewFileResources: %v", err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if err := res.Write("node.yaml", []byte("data")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	fi, err := os.Stat(filepath.Join(root, "node.yaml"))
	if err != nil {
		t.Fatalf("Stat written file: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("written file mode = %o, want 600", got)
	}
	data, err := res.Read("node.yaml")
	if err != nil || string(data) != "data" {
		t.Fatalf("Read(node.yaml) = (%q, %v), want (%q, nil)", data, err, "data")
	}
}

func TestFileResourcesDataRoot(t *testing.T) {
	res, err := NewFileResources(t.TempDir(), true)
	if err != nil {
		t.Fatalf("NewFileResources: %v", err)
	}

	if got, want := res.DataRoot(), res.Root(); got != want {
		t.Fatalf("DataRoot() before SetDataRoot = %q, want Root() %q", got, want)
	}

	res.SetDataRoot("/d")
	if got := res.DataRoot(); got != "/d" {
		t.Fatalf("DataRoot() after SetDataRoot = %q, want %q", got, "/d")
	}
}
