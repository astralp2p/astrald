package objects

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// trackedLeaf is the registered form of a leaf repository. It passes every call to the leaf and hands commits,
// reads and deletes to the tracker.
// why: only the leaf sees every commit together with its leaf name, because a group passes each write to a member.
type trackedLeaf struct {
	objectsmod.Repository
	name    string
	tracker *tracker
}

var _ objectsmod.Repository = &trackedLeaf{}
var _ objectsmod.AfterRemovedCallback = &trackedLeaf{}

// addLeaf registers repo under name in its tracked form and returns that form. Every leaf repository is registered here.
func (mod *Module) addLeaf(name string, repo objectsmod.Repository) (objectsmod.Repository, error) {
	leaf := &trackedLeaf{Repository: repo, name: name, tracker: &mod.tracker}

	_, ok := mod.repos.Set(name, leaf)
	if !ok {
		return nil, fmt.Errorf("repo %v already added", repo.Label())
	}
	mod.tracker.add(name, repo)
	return leaf, nil
}

// Create opens a writer whose successful commit is tracked.
func (leaf *trackedLeaf) Create(ctx *astral.Context, opts *objectsmod.CreateOpts) (objects.Writer, error) {
	w, err := leaf.Repository.Create(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &trackedWriter{Writer: w, leaf: leaf.name, tracker: leaf.tracker}, nil
}

// Read opens the object in the leaf through a reader that counts the read.
func (leaf *trackedLeaf) Read(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	r, err := leaf.Repository.Read(ctx, objectID, offset, limit)
	if err != nil {
		return nil, err
	}
	return leaf.tracker.openReader(ctx, r, offset), nil
}

// Delete deletes the object under the leaf's lock and removes its row.
func (leaf *trackedLeaf) Delete(ctx *astral.Context, objectID *astral.ObjectID) error {
	mu := leaf.tracker.leafLock(leaf.name)
	mu.Lock()
	defer mu.Unlock()

	return leaf.tracker.deleteLocked(ctx, leaf.name, leaf.Repository, objectID)
}

// AfterRemoved stops tracking the leaf and forwards the callback to the leaf.
func (leaf *trackedLeaf) AfterRemoved(name string) {
	leaf.tracker.forget(leaf.name)

	if c, ok := leaf.Repository.(objectsmod.AfterRemovedCallback); ok {
		c.AfterRemoved(name)
	}
}

// trackedWriter commits under the leaf's lock and tracks the commit.
type trackedWriter struct {
	objects.Writer
	leaf    string
	tracker *tracker
}

// Commit commits the object and writes its commit time, also when the content already existed.
// note: the commit refreshes the time so that purge cannot delete fresh content; accepted by the operator for now and may change later.
// note: no Commit runs inside an open database transaction, because the node has one database connection.
func (w *trackedWriter) Commit() (*astral.ObjectID, error) {
	mu := w.tracker.leafLock(w.leaf)
	mu.Lock()
	defer mu.Unlock()

	objectID, err := w.Writer.Commit()
	if err != nil {
		return nil, err
	}

	w.tracker.committed(w.leaf, objectID)
	return objectID, nil
}

// recountInterval is how often a stream that keeps delivering bytes is counted again.
const recountInterval = time.Minute

// countedReader counts a read in the tracker's journal.
// A read counts when its first byte is delivered, or when it closes if it can deliver no byte (operator decision 2026-11-12),
// and again once a minute while bytes keep coming.
// note: it claims no io.Seeker; a caller that seeks wraps it in objectsmod.ReadSeeker, which reopens through the repository.
type countedReader struct {
	objectsmod.Reader
	tracker *tracker

	counted   atomic.Bool // false for a housekeeping read, until count is called
	delivered atomic.Bool // a byte reached the reader's caller
	zeroOnly  bool        // the read window holds no byte

	mu       sync.Mutex
	markedAt time.Time // zero until the read is counted
	closed   atomic.Bool
}

// Read forwards to the inner reader and counts the read when it delivers a byte.
func (r *countedReader) Read(p []byte) (int, error) {
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
func (r *countedReader) count() {
	r.counted.Store(true)
	if r.delivered.Load() {
		r.mark()
	}
}

// Close closes the inner reader and counts a read that can deliver no byte.
func (r *countedReader) Close() error {
	err := r.Reader.Close()
	if r.closed.CompareAndSwap(false, true) && r.zeroOnly {
		r.mark()
	}
	return err
}

// mark notes the opened object in the journal when the read is due: not counted yet, or last counted a minute ago.
func (r *countedReader) mark() {
	if !r.counted.Load() {
		return
	}

	if r.markDue(r.tracker.journal.now()) {
		r.tracker.markRead(r.Reader.ID())
	}
}

// markDue reports whether the read is due at now, and records now when it is.
func (r *countedReader) markDue(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	due := r.markedAt.IsZero() || now.Sub(r.markedAt) >= recountInterval
	if due {
		r.markedAt = now
	}
	return due
}
