package objects

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// trackedRepository wraps a leaf repository and keeps its rows in the tracking table.
// note: the rules are in VFS 6, §11 "objects: tracking": https://wiki.satforge.dev/doc/vfs-6-specification-BxBnrSeTfT
type trackedRepository struct {
	mod   *Module
	name  string
	inner objectsmod.Repository

	// mu is the leaf's lock. Commit and Delete hold it across the inner call and the tracking write.
	// note: purge holds mu for each candidate from its re-check to its delete and deletes through deleteLocked,
	// so commit, delete and purge never interleave, and a commit that answered cannot lose to a purge (VFS 6, §11 "objects: purge").
	mu sync.Mutex
}

var _ objectsmod.Repository = &trackedRepository{}
var _ objectsmod.AfterRemovedCallback = &trackedRepository{}

// addLeaf wraps repo and registers the wrapper under name. Every leaf repository is registered here.
func (mod *Module) addLeaf(name string, repo objectsmod.Repository) (*trackedRepository, error) {
	tracked := &trackedRepository{mod: mod, name: name, inner: repo}

	_, ok := mod.repos.Set(name, tracked)
	if !ok {
		return nil, fmt.Errorf("repo %v already added", repo.Label())
	}
	return tracked, nil
}

// Inner returns the wrapped repository.
func (repo *trackedRepository) Inner() objectsmod.Repository {
	return repo.inner
}

func (repo *trackedRepository) Label() string {
	return repo.inner.Label()
}

// Create opens a writer whose successful commit writes a tracking row.
func (repo *trackedRepository) Create(ctx *astral.Context, opts *objectsmod.CreateOpts) (objects.Writer, error) {
	w, err := repo.inner.Create(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &trackedWriter{Writer: w, repo: repo}, nil
}

func (repo *trackedRepository) Contains(ctx *astral.Context, objectID *astral.ObjectID) (bool, error) {
	return repo.inner.Contains(ctx, objectID)
}

func (repo *trackedRepository) Scan(ctx *astral.Context, follow bool) (<-chan *astral.ObjectID, error) {
	return repo.inner.Scan(ctx, follow)
}

// Delete deletes the object and then removes its tracking row.
func (repo *trackedRepository) Delete(ctx *astral.Context, objectID *astral.ObjectID) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	return repo.deleteLocked(ctx, objectID)
}

// deleteLocked deletes the object and then removes its tracking row. The caller holds mu.
func (repo *trackedRepository) deleteLocked(ctx *astral.Context, objectID *astral.ObjectID) error {
	resolved, err := repo.resolve(ctx, objectID)
	if err != nil {
		return err
	}

	err = repo.inner.Delete(ctx, objectID)
	if err != nil {
		return err
	}

	err = repo.mod.db.deleteTracking(repo.name, resolved)
	if err != nil {
		repo.mod.log.Error("untrack %v in %v: %v", resolved, repo.name, err)
	}
	return nil
}

// resolve returns the full ID of the object a partial objectID names.
// why: a row is keyed by the full ID, and only the repository knows which object a partial ID names.
func (repo *trackedRepository) resolve(ctx *astral.Context, objectID *astral.ObjectID) (*astral.ObjectID, error) {
	if objectID.Size != 0 {
		return objectID, nil
	}

	r, err := repo.inner.Read(ctx, objectID, 0, 0)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return r.ID(), nil
}

// Read opens the object through the inner repository. The reader names the wrapper as its repository.
// The reader counts the read in the journal, unless ctx is housekeeping.
// note: the rules are in VFS 6, §11 "objects: last touched": https://wiki.satforge.dev/doc/vfs-6-specification-BxBnrSeTfT
func (repo *trackedRepository) Read(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	r, err := repo.inner.Read(ctx, objectID, offset, limit)
	if err != nil {
		return nil, err
	}

	tr := &trackedReader{Reader: r, repo: repo}
	tr.counted.Store(!objectsmod.IsHousekeeping(ctx))

	// why: an object smaller than or equal to the offset delivers no byte, and the operator counts such a read when it closes.
	tr.zeroOnly = offset >= 0 && uint64(offset) >= r.ID().Size

	// why: the wrapper claims io.Seeker only where the inner reader seeks, so a caller that tests for a Seeker is not misled.
	if _, ok := r.(io.Seeker); ok {
		return &trackedSeekReader{trackedReader: tr}, nil
	}
	return tr, nil
}

func (repo *trackedRepository) Free(ctx *astral.Context) (int64, error) {
	return repo.inner.Free(ctx)
}

// AfterRemoved removes the leaf's tracking rows and forwards the callback to the inner repository.
func (repo *trackedRepository) AfterRemoved(name string) {
	err := repo.mod.db.deleteRepositoryTracking(repo.name)
	if err != nil {
		repo.mod.log.Error("untrack repository %v: %v", repo.name, err)
	}

	if c, ok := repo.inner.(objectsmod.AfterRemovedCallback); ok {
		c.AfterRemoved(name)
	}
}

// track writes the commit time of objectID.
// note: a failed tracking write leaves the commit successful and is logged; an untracked object is never purged.
// note: operator decision 2026-11-12, recorded in https://wiki.satforge.dev/doc/implement-the-vfs-module-and-the-changes-that-ship-with-it-oofFdNw03v
func (repo *trackedRepository) track(objectID *astral.ObjectID) {
	err := repo.mod.db.trackCommit(repo.name, objectID, astral.Now().Time().Unix())
	if err != nil {
		repo.mod.log.Error("track %v in %v: %v", objectID, repo.name, err)
	}
}

// trackedWriter writes a tracking row after a successful commit.
type trackedWriter struct {
	objects.Writer
	repo *trackedRepository
}

// Commit commits the object and writes its commit time, also when the content already existed.
// note: the commit refreshes the time so that purge cannot delete fresh content; accepted by the operator for now and may change later.
// note: no Commit runs inside an open database transaction, because the node has one database connection.
func (w *trackedWriter) Commit() (*astral.ObjectID, error) {
	w.repo.mu.Lock()
	defer w.repo.mu.Unlock()

	objectID, err := w.Writer.Commit()
	if err != nil {
		return nil, err
	}

	w.repo.track(objectID)
	return objectID, nil
}

// recountInterval is how often a stream that keeps delivering bytes is counted again.
const recountInterval = time.Minute

// trackedReader names the wrapper as the repository of an opened object and counts the read.
// why: the name lookup compares a reader's repository with the registered one, which is the wrapper.
// A read counts when its first byte is delivered, or when it closes if it can deliver no byte (operator decision 2026-11-12),
// and again once a minute while bytes keep coming.
type trackedReader struct {
	objectsmod.Reader
	repo *trackedRepository

	counted   atomic.Bool // false for a housekeeping read, until count is called
	delivered atomic.Bool // a byte reached the reader's caller
	zeroOnly  bool        // the read window holds no byte

	mu       sync.Mutex
	markedAt time.Time // zero until the read is counted
	closed   atomic.Bool
}

func (r *trackedReader) Repo() objectsmod.Repository {
	return r.repo
}

// Read forwards to the inner reader and counts the read when it delivers a byte.
func (r *trackedReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 {
		r.delivered.Store(true)
		r.mark()
	}
	return n, err
}

// count makes a read opened as housekeeping count from now on, and counts the bytes it delivered so far.
// why: a read that races other candidates opens uncounted, and only the winner counts once its first byte is delivered to the caller.
// note: the rules are in VFS 6, §11 "objects: last touched": https://wiki.satforge.dev/doc/vfs-6-specification-BxBnrSeTfT
func (r *trackedReader) count() {
	r.counted.Store(true)
	if r.delivered.Load() {
		r.mark()
	}
}

// Close closes the inner reader and counts a read that can deliver no byte.
func (r *trackedReader) Close() error {
	err := r.Reader.Close()
	if r.closed.CompareAndSwap(false, true) && r.zeroOnly {
		r.mark()
	}
	return err
}

// mark notes the opened object in the journal when the read is due: not counted yet, or last counted a minute ago.
// note: marking does no database work.
func (r *trackedReader) mark() {
	if !r.counted.Load() {
		return
	}

	journal := r.repo.mod.objectsReadsJournal
	now := journal.now()

	if r.markDue(now) {
		journal.Mark(r.Reader.ID())
	}
}

// markDue reports whether the read is due at now, and records now when it is.
func (r *trackedReader) markDue(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	due := r.markedAt.IsZero() || now.Sub(r.markedAt) >= recountInterval
	if due {
		r.markedAt = now
	}
	return due
}

// trackedSeekReader is a trackedReader over an inner reader that seeks.
// why: the HTTP object handler seeks the reader it serves.
type trackedSeekReader struct {
	*trackedReader
}

var _ io.Seeker = &trackedSeekReader{}

// Seek forwards to the inner reader, which seeks.
func (r *trackedSeekReader) Seek(offset int64, whence int) (int64, error) {
	return r.Reader.(io.Seeker).Seek(offset, whence)
}
