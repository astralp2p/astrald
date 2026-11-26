package vfs

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// fileTestObjects serves reads from the test repository the module writes to.
type fileTestObjects struct {
	*dirTestObjects
}

func (o *fileTestObjects) GetRepository(name string) objectsmod.Repository {
	if name != readRepository {
		return nil
	}

	return &fileTestRepo{dirTestRepo: o.repo}
}

// fileTestRepo reads back what the test repository committed.
type fileTestRepo struct {
	*dirTestRepo
}

func (r *fileTestRepo) Read(_ *astral.Context, id *astral.ObjectID, _, _ int64) (objectsmod.Reader, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, ok := r.objects[*id]
	if !ok {
		return nil, objectsmod.ErrNotFound
	}

	return &readOpsReader{Reader: bytes.NewReader(data), id: *id}, nil
}

func newFileTestModule(t *testing.T) (*Module, *astral.Identity) {
	t.Helper()

	mod := testModule(t)
	mod.Objects = &fileTestObjects{dirTestObjects: mod.Objects.(*dirTestObjects)}

	id := astral.GenerateIdentity()
	createVFS(t, mod, id)

	return mod, id
}

// TestWriteFileReadsBackAndLists: a file written in a new folder reads back byte for byte, its ObjectID is the
// ObjectID of its content, and the folders list their names in byte order.
func TestWriteFileReadsBackAndLists(t *testing.T) {
	mod, id := newFileTestModule(t)
	ctx := astral.NewContext(nil)

	objectID, err := mod.WriteFile(ctx, id, "/a/b/two", []byte("second"))
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if want := contentID("second"); *objectID != want {
		t.Fatalf("ObjectID = %v, want %v", objectID, want)
	}
	if _, err = mod.WriteFile(ctx, id, "/a/b/one", []byte("first")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := mod.ReadFile(ctx, id, "/a/b/two")
	if err != nil || string(got) != "second" {
		t.Fatalf("ReadFile = %q, %v; want second", got, err)
	}

	names, err := mod.ListFolder(id, "/a/b")
	if err != nil || !slices.Equal(names, []string{"one", "two"}) {
		t.Fatalf("ListFolder = %v, %v; want [one two]", names, err)
	}

	if names, err = mod.ListFolder(id, "/"); err != nil || !slices.Equal(names, []string{"a"}) {
		t.Fatalf("ListFolder(/) = %v, %v; want [a]", names, err)
	}
}

// TestWriteFileRefusals: a missing VFS, empty content, a folder at the path and a missing path are refused.
func TestWriteFileRefusals(t *testing.T) {
	mod, id := newFileTestModule(t)
	ctx := astral.NewContext(nil)

	if _, err := mod.WriteFile(ctx, astral.GenerateIdentity(), "/x", []byte("x")); !errors.Is(err, vfsapi.ErrNoSuchVFS) {
		t.Fatalf("missing VFS: err = %v, want ErrNoSuchVFS", err)
	}
	if _, err := mod.WriteFile(ctx, id, "/x", nil); !errors.Is(err, errEmptyFile) {
		t.Fatalf("empty content: err = %v, want errEmptyFile", err)
	}
	if _, err := mod.WriteFile(ctx, id, "/d/f", []byte("x")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := mod.WriteFile(ctx, id, "/d", []byte("x")); !errors.Is(err, vfsapi.ErrIsAFolder) {
		t.Fatalf("folder at path: err = %v, want ErrIsAFolder", err)
	}
	if _, err := mod.ReadFile(ctx, id, "/nope"); !errors.Is(err, vfsapi.ErrNotFound) {
		t.Fatalf("ReadFile missing: err = %v, want ErrNotFound", err)
	}
	if _, err := mod.ListFolder(id, "/nope"); !errors.Is(err, vfsapi.ErrNotFound) {
		t.Fatalf("ListFolder missing: err = %v, want ErrNotFound", err)
	}
	if _, err := mod.ListFolder(id, "/d/f"); !errors.Is(err, vfsapi.ErrNotAFolder) {
		t.Fatalf("ListFolder file: err = %v, want ErrNotAFolder", err)
	}
}
