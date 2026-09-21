package fs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// Commit renames the temp file to the content-addressed name and reports that ID.
func TestWriter_Commit_StoresObject(t *testing.T) {
	root := t.TempDir()
	data := []byte("hello")

	id, err := newWriterCommit(t, root, data)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	want := resolveID(t, data)
	if id.String() != want.String() {
		t.Fatalf("want %v, got %v", want, id)
	}

	stored, err := os.ReadFile(filepath.Join(root, want.String()))
	if err != nil {
		t.Fatalf("read object: %v", err)
	}
	if !bytes.Equal(stored, data) {
		t.Fatalf("want %v, got %v", data, stored)
	}
	assertNoTempFile(t, root)
}

// Commit deduplicates against an object that is already stored and drops the temp file.
func TestWriter_Commit_Deduplicates(t *testing.T) {
	root := t.TempDir()
	data := []byte("hello")
	want := resolveID(t, data)

	if err := os.WriteFile(filepath.Join(root, want.String()), data, 0o600); err != nil {
		t.Fatal(err)
	}

	id, err := newWriterCommit(t, root, data)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if id.String() != want.String() {
		t.Fatalf("want %v, got %v", want, id)
	}
	assertNoTempFile(t, root)
}

// Commit reports the rename error itself when the object's name is taken by a directory,
// and never answers a nil ObjectID with a nil error.
func TestWriter_Commit_RenameFails(t *testing.T) {
	root := t.TempDir()
	data := []byte("hello")

	// a non-empty directory holds the object's final name, so the rename cannot succeed
	blocked := filepath.Join(root, resolveID(t, data).String())
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "occupied"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	id, err := newWriterCommit(t, root, data)

	if id != nil {
		t.Fatalf("want a nil ObjectID, got %v", id)
	}
	if err == nil {
		t.Fatal("want the rename error, got nil")
	}

	var linkErr *os.LinkError
	if !errors.As(err, &linkErr) || linkErr.Op != "rename" {
		t.Fatalf("want the rename error, got %v", err)
	}
	assertNoTempFile(t, root)
}

func newWriterCommit(t *testing.T, root string, data []byte) (*astral.ObjectID, error) {
	t.Helper()

	w, err := NewWriter(NewRepository(nil, "test", root), root)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	if _, err = w.Write(data); err != nil {
		t.Fatalf("write: %v", err)
	}

	return w.Commit()
}

func resolveID(t *testing.T, data []byte) *astral.ObjectID {
	t.Helper()

	id, err := astral.Resolve(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	return id
}

func assertNoTempFile(t *testing.T, root string) {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tempFilePrefix) {
			t.Fatalf("temp file left behind: %v", e.Name())
		}
	}
}
