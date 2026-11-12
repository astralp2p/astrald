package fs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// writeTempAndObject writes a temp file of an interrupted write and an object file into root.
func writeTempAndObject(t *testing.T, root string) (temp string, object string) {
	t.Helper()

	id := resolveID(t, []byte("hello"))
	temp = filepath.Join(root, tempFilePrefix+"0011223344556677")
	object = filepath.Join(root, id.String())
	for path, data := range map[string]string{temp: "partial", object: "hello"} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return temp, object
}

// A registered repository removes temp files of interrupted writes and keeps object files.
func TestAddRepository_RemovesTempFiles(t *testing.T) {
	mod, _ := newTreeModule(t)
	root := t.TempDir()
	temp, object := writeTempAndObject(t, root)

	if err := mod.addRepository("test", NewRepository(mod, "test", root)); err != nil {
		t.Fatalf("addRepository: %v", err)
	}

	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatalf("temp file still exists: %v", err)
	}
	if _, err := os.Stat(object); err != nil {
		t.Fatalf("object file removed: %v", err)
	}
}

// refusingObjects refuses every registration, as the objects module refuses a name already registered.
type refusingObjects struct {
	memObjects
}

var errRefused = errors.New("repo already added")

func (o *refusingObjects) AddRepository(string, objectsmod.Repository) error {
	return errRefused
}

// A refused registration deletes nothing, so a live repository on the same root keeps its writes in flight.
func TestAddRepository_RefusedKeepsTempFiles(t *testing.T) {
	mod, _ := newTreeModule(t)
	mod.Objects = &refusingObjects{memObjects{repos: map[string]objectsmod.Repository{}}}
	root := t.TempDir()
	temp, object := writeTempAndObject(t, root)

	if err := mod.addRepository("live", NewRepository(mod, "live", root)); !errors.Is(err, errRefused) {
		t.Fatalf("addRepository = %v, want errRefused", err)
	}

	for _, path := range []string{temp, object} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%v removed by a refused registration: %v", path, err)
		}
	}
}

// NewRepository alone deletes nothing; cleanup belongs to registration.
func TestNewRepository_KeepsTempFiles(t *testing.T) {
	root := t.TempDir()
	temp, _ := writeTempAndObject(t, root)

	NewRepository(nil, "test", root)

	if _, err := os.Stat(temp); err != nil {
		t.Fatalf("temp file removed: %v", err)
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

// A repository registered from the config file or restored from the tree removes its temp files.
func TestAddConfigAndTreeRepos_RemoveTempFiles(t *testing.T) {
	mod, _ := newTreeModule(t)
	configRoot, treeRoot := t.TempDir(), t.TempDir()
	configTemp, _ := writeTempAndObject(t, configRoot)
	treeTemp, _ := writeTempAndObject(t, treeRoot)

	mod.config.Repos = map[string]RepoConfig{"configured": {Label: "configured", Path: configRoot, Writable: true}}
	mod.saveRepo(mod.ctx, "restored", RepoConfig{Label: "restored", Path: treeRoot, Writable: true})

	mod.addConfigRepos()
	mod.addTreeRepos(mod.ctx)

	for _, temp := range []string{configTemp, treeTemp} {
		if _, err := os.Stat(temp); !os.IsNotExist(err) {
			t.Fatalf("temp file %v still exists: %v", temp, err)
		}
	}
}
