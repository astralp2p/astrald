package fs

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// NewRepository removes temp files of interrupted writes and keeps object files.
func TestNewRepository_RemovesTempFiles(t *testing.T) {
	root := t.TempDir()
	id := resolveID(t, []byte("hello"))

	temp := filepath.Join(root, tempFilePrefix+"0011223344556677")
	object := filepath.Join(root, id.String())
	for path, data := range map[string]string{temp: "partial", object: "hello"} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	NewRepository(nil, "test", root)

	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatalf("temp file still exists: %v", err)
	}
	if _, err := os.Stat(object); err != nil {
		t.Fatalf("object file removed: %v", err)
	}
}

// Contains is true only when the file size equals the size in the ObjectID.
func TestRepository_Contains_SizeMismatch(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "test", root)
	id := resolveID(t, []byte("hello"))

	if err := os.WriteFile(filepath.Join(root, id.String()), []byte("hel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.Contains(nil, id); err != nil || ok {
		t.Fatalf("truncated file: want false, got %v, %v", ok, err)
	}

	if err := os.WriteFile(filepath.Join(root, id.String()), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.Contains(nil, id); err != nil || !ok {
		t.Fatalf("whole file: want true, got %v, %v", ok, err)
	}
}

// A commit replaces an existing file of the wrong size, and the content reads back whole.
func TestWriter_Commit_ReplacesWrongSizeFile(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "test", root)
	data := []byte("hello")
	id := resolveID(t, data)
	path := filepath.Join(root, id.String())

	if err := os.WriteFile(path, []byte("hel"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := repo.Create(astral.NewContext(nil), nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := got.Write(data); err != nil {
		t.Fatalf("write: %v", err)
	}
	committed, err := got.Commit()
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if committed.String() != id.String() {
		t.Fatalf("want %v, got %v", id, committed)
	}

	r, err := repo.Read(astral.NewContext(nil), id, 0, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	defer r.Close()

	content, err := io.ReadAll(r)
	if err != nil || string(content) != "hello" {
		t.Fatalf("want hello, got %q, %v", content, err)
	}
	assertNoTempFile(t, root)
}
