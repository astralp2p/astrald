package resources

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestNewFileResourcesMkdirFalseDoesNotCreate: mkdir=false must not create the
// root. os.Stat always returns *fs.PathError, so the old guard was dead and
// MkdirAll ran regardless of the flag.
func TestNewFileResourcesMkdirFalseDoesNotCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")

	res, err := NewFileResources(path, false)
	if res != nil || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("NewFileResources(missing, false) = (%v, %v), want (nil, fs.ErrNotExist)", res, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("NewFileResources(missing, false) created %q", path)
	}
}

// TestNewFileResourcesMkdirTrueStillCreates pins that every existing caller,
// all of which pass true, sees exactly what it saw before.
func TestNewFileResourcesMkdirTrueStillCreates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new")

	if _, err := NewFileResources(path, true); err != nil {
		t.Fatalf("NewFileResources(missing, true) = %v, want nil", err)
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		t.Fatalf("root %q not created as a directory: %v", path, err)
	}
}
