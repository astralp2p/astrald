package objects

import (
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/objects/mem"
)

// endTrackingWriter counts how a writer was ended.
type endTrackingWriter struct {
	objects.Writer
	commits  atomic.Int32
	discards atomic.Int32
}

func (w *endTrackingWriter) Commit() (*astral.ObjectID, error) {
	w.commits.Add(1)
	return w.Writer.Commit()
}

func (w *endTrackingWriter) Discard() error {
	w.discards.Add(1)
	return w.Writer.Discard()
}

// endTrackingRepo hands out endTrackingWriters over an in-memory repository.
type endTrackingRepo struct {
	*mem.Repository
	writers []*endTrackingWriter
}

func (r *endTrackingRepo) Create(ctx *astral.Context, opts *objectsmod.CreateOpts) (objects.Writer, error) {
	w, err := r.Repository.Create(ctx, opts)
	if err != nil {
		return nil, err
	}
	rec := &endTrackingWriter{Writer: w}
	r.writers = append(r.writers, rec)
	return rec, nil
}

var errStoreEncode = errors.New("encode failed")

// failingObject fails to encode, which is the path that left the writer unended.
type failingObject struct{}

func (failingObject) ObjectType() string                { return "test.objects.failing" }
func (failingObject) WriteTo(io.Writer) (int64, error)  { return 0, errStoreEncode }
func (failingObject) ReadFrom(io.Reader) (int64, error) { return 0, errStoreEncode }

// TestModule_StoreDiscardsTheWriterWhenEncodingFails: a failed encode ends the
// writer, so no reservation and no temp file survive the call.
//
// why a zero Module suffices: Store returns at the encode error, before
// trackObject reaches mod.db.
func TestModule_StoreDiscardsTheWriterWhenEncodingFails(t *testing.T) {
	repo := &endTrackingRepo{Repository: mem.New("test", 0)}
	mod := &Module{}

	id, err := mod.Store(astral.NewContext(nil), repo, failingObject{})

	if !errors.Is(err, errStoreEncode) {
		t.Fatalf("Store err = %v; want %v", err, errStoreEncode)
	}
	if id != nil {
		t.Errorf("Store returned id %v for a failed encode", id)
	}
	if len(repo.writers) != 1 {
		t.Fatalf("Store created %d writers; want 1", len(repo.writers))
	}
	if got := repo.writers[0].discards.Load(); got != 1 {
		t.Errorf("writer discarded %d times; want 1 — a failed encode left it unended", got)
	}
	if got := repo.writers[0].commits.Load(); got != 0 {
		t.Errorf("writer committed %d times; want 0", got)
	}
	if got := repo.Used(); got != 0 {
		t.Errorf("repository still counts %d bytes after a failed store", got)
	}
}

// TestModule_StoreDiscardAfterCommitIsHarmless: the deferred Discard runs on the
// success path too and must not undo the commit.
//
// why Commit is driven directly: Module.Store reaches trackObject after a
// successful commit, which needs a database this test does not build.
func TestModule_StoreDiscardAfterCommitIsHarmless(t *testing.T) {
	ctx := astral.NewContext(nil)
	repo := &endTrackingRepo{Repository: mem.New("test", 0)}

	w, err := repo.Create(ctx, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	id, err := w.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := w.Discard(); err != nil {
		t.Fatalf("Discard after Commit: %v", err)
	}

	has, err := repo.Contains(ctx, id)
	if err != nil {
		t.Fatalf("Contains: %v", err)
	}
	if !has {
		t.Error("a Discard after a successful Commit removed the object")
	}
}
