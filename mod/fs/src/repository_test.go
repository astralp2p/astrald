package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

// Delete maps a missing-file os.Remove error to objects.ErrNotFound so purge skips the leaf.
func TestRepository_Delete_MissingFile(t *testing.T) {
	repo := NewRepository(nil, "test", t.TempDir())
	id := &astral.ObjectID{Size: 1}

	err := repo.Delete(nil, id)

	if !errors.Is(err, objects.ErrNotFound) {
		t.Fatalf("want objects.ErrNotFound, got %v", err)
	}
}

// Delete removes an existing object file and returns no error.
func TestRepository_Delete_ExistingFile(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "test", root)
	id := &astral.ObjectID{Size: 1}

	path := filepath.Join(root, id.String())
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := repo.Delete(nil, id); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}
