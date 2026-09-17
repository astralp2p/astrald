package fs

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
)

func createWriter(t *testing.T, repo *Repository) objects.Writer {
	t.Helper()

	w, err := repo.Create(astral.NewContext(nil), nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return w
}

func commitString(t *testing.T, repo *Repository, data string) *astral.ObjectID {
	t.Helper()

	w := createWriter(t, repo)
	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatalf("Write(%q): %v", data, err)
	}
	id, err := w.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if id == nil {
		t.Fatal("Commit returned a nil id")
	}
	return id
}

func resolveString(t *testing.T, data string) *astral.ObjectID {
	t.Helper()

	id, err := astral.Resolve(bytes.NewReader([]byte(data)))
	if err != nil {
		t.Fatalf("Resolve(%q): %v", data, err)
	}
	return id
}

func dirNames(t *testing.T, dir string) (objectFiles, tempFiles []string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), tempFilePrefix) {
			tempFiles = append(tempFiles, entry.Name())
			continue
		}
		objectFiles = append(objectFiles, entry.Name())
	}
	return objectFiles, tempFiles
}

func TestWriter_CommitRenamesToObjectID(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "t", root)

	id := commitString(t, repo, "hello")

	if want := resolveString(t, "hello"); *id != *want {
		t.Fatalf("Commit id: got %v, want %v", id, want)
	}

	info, err := os.Stat(filepath.Join(root, id.String()))
	if err != nil {
		t.Fatalf("stat object file: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Errorf("object file mode: got %v, want a regular file", info.Mode())
	}

	if _, temps := dirNames(t, root); len(temps) != 0 {
		t.Errorf("temp files after Commit: got %v, want none", temps)
	}
}

func TestWriter_CommitDuplicateContent(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "t", root)

	first := commitString(t, repo, "hello")
	second := commitString(t, repo, "hello")

	if *first != *second {
		t.Fatalf("second Commit id: got %v, want %v", second, first)
	}

	objectFiles, temps := dirNames(t, root)
	if len(objectFiles) != 1 || objectFiles[0] != first.String() {
		t.Errorf("object files: got %v, want [%v]", objectFiles, first)
	}
	if len(temps) != 0 {
		t.Errorf("temp files: got %v, want none", temps)
	}
}

func TestWriter_Discard(t *testing.T) {
	root := t.TempDir()
	repo := NewRepository(nil, "t", root)

	w := createWriter(t, repo)
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, temps := dirNames(t, root); len(temps) != 1 {
		t.Fatalf("temp files before Discard: got %v, want exactly one", temps)
	}

	if err := w.Discard(); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	objectFiles, temps := dirNames(t, root)
	if len(objectFiles) != 0 || len(temps) != 0 {
		t.Errorf("files after Discard: got objects %v and temps %v, want none", objectFiles, temps)
	}

	if _, err := w.Commit(); err == nil || err.Error() != "writer closed" {
		t.Errorf("Commit after Discard: got %v, want \"writer closed\"", err)
	}
}
