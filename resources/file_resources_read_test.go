package resources

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestFileResourcesReadMissingIsErrNotFound pins the first-run signal that
// loadNodeIdentity relies on to generate a node key.
func TestFileResourcesReadMissingIsErrNotFound(t *testing.T) {
	res, err := NewFileResources(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := res.Read("node_key"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Read(missing) = %v, want ErrNotFound", err)
	}
}

// TestFileResourcesReadOtherErrorIsNotErrNotFound: a read error that is not
// fs.ErrNotExist must not become ErrNotFound. The old string match did map it
// whenever the error text contained "no such file or directory", here via the
// resource name.
func TestFileResourcesReadOtherErrorIsNotErrNotFound(t *testing.T) {
	root := t.TempDir()
	const name = "no such file or directory"
	if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
		t.Fatal(err)
	}

	res, err := NewFileResources(root, false)
	if err != nil {
		t.Fatal(err)
	}

	_, err = res.Read(name)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("Read(directory) = %v, want a non-ErrNotFound error", err)
	}
}
