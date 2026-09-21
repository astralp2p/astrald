package fs

import (
	"errors"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

var _ objectsmod.Repository = &Repository{}

// Repository is a filesystem-backed object store rooted at a single directory.
// addQueue broadcasts newly committed object IDs to active Scan followers.
// ids holds the candidate full IDs of each hash for partial lookup; nil means the index is not built yet.
type Repository struct {
	mod      *Module
	label    string
	root     string
	mu       sync.Mutex // guards addQueue, ids, and the file moves and removals ids follows
	addQueue *sig.Queue[*astral.ObjectID]
	ids      *sig.Map[[32]byte, []astral.ObjectID]
}

var _ objectsmod.Repository = &Repository{}

func NewRepository(mod *Module, label string, path string) *Repository {
	return &Repository{
		mod:      mod,
		label:    label,
		root:     path,
		addQueue: &sig.Queue[*astral.ObjectID]{},
	}
}

// Scan emits existing objects from the root directory, then — when follow is true — sends a nil
// sentinel to mark the end of the snapshot before streaming newly added object IDs.
func (repo *Repository) Scan(ctx *astral.Context, follow bool) (<-chan *astral.ObjectID, error) {
	ch := make(chan *astral.ObjectID)

	var subscribe <-chan *astral.ObjectID

	go func() {
		defer close(ch)

		if follow {
			subscribe = sig.Subscribe(ctx, repo.added())
		}

		entries, err := os.ReadDir(repo.root)
		if err != nil {
			repo.mod.log.Error("cannot read dir %v: %v", repo.root, err)
			return
		}

		for _, entry := range entries {
			objectID := entryObjectID(entry)
			if objectID == nil {
				continue
			}

			select {
			case ch <- objectID:
			case <-ctx.Done():
				return
			}

		}

		// handle subscription
		if subscribe != nil {
			select {
			case <-ctx.Done():
				return
			case ch <- nil:
			}

			for id := range subscribe {
				select {
				case ch <- id:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}

// Read opens the object file for reading, applying the given offset and limit.
// Rejects requests from outside ZoneDevice to prevent unintended network exposure.
func (repo *Repository) Read(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	if !ctx.Zone().Is(astral.ZoneDevice) {
		return nil, astral.ErrZoneExcluded
	}

	resolved, err := repo.findObject(ctx, objectID)
	if err != nil {
		return nil, err
	}

	f, err := repo.openObject(resolved)
	if err != nil {
		// why: only a partial ID resolves through the index, so only its miss can leave a stale entry.
		// why: a full-ID miss takes no lock, so it never waits behind a partial lookup's directory listing.
		if objectID.Size == 0 {
			repo.evict(resolved)
		}
		return nil, err
	}

	n, err := objectsmod.ResolveReadLimit(resolved, offset, limit)
	if err != nil {
		f.Close()
		return nil, err
	}

	if offset != 0 {
		pos, err := f.Seek(offset, io.SeekStart)
		if err != nil || pos != offset {
			f.Close()
			return nil, objectsmod.ErrNotFound
		}
	}

	return NewReader(f, resolved, n, repo), nil
}

// openObject opens the file of a resolved full ID and checks that the opened file is a regular file of its size.
// A file that cannot be opened or no longer matches is reported as ErrNotFound.
func (repo *Repository) openObject(objectID *astral.ObjectID) (*os.File, error) {
	f, err := os.Open(filepath.Join(repo.root, objectID.String()))
	if err != nil {
		return nil, objectsmod.ErrNotFound
	}

	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || uint64(stat.Size()) != objectID.Size {
		f.Close()
		return nil, objectsmod.ErrNotFound
	}

	return f, nil
}

// Contains reports whether the repository holds the object.
// A full ID checks its file alone; a partial ID resolves through the hash index.
func (repo *Repository) Contains(ctx *astral.Context, objectID *astral.ObjectID) (bool, error) {
	if objectID.Size == 0 {
		return repo.containsPartial(ctx, objectID)
	}

	path := filepath.Join(repo.root, objectID.String())

	// check if we have the file
	f, err := os.Stat(path)
	if err != nil {
		return false, nil
	}

	// and it's a regular file
	if f.Mode().IsRegular() {
		return true, nil
	}
	return false, nil
}

// containsPartial reports whether a partial ID resolves to a stored object.
// A miss is false; an ambiguous hash and a filesystem error are returned.
func (repo *Repository) containsPartial(ctx *astral.Context, objectID *astral.ObjectID) (bool, error) {
	_, err := repo.findObject(ctx, objectID)
	switch {
	case errors.Is(err, objectsmod.ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

func (repo *Repository) Label() string {
	return repo.label
}

// Create opens a new object writer, refusing if opts requests more space than is currently free.
func (repo *Repository) Create(ctx *astral.Context, opts *objectsmod.CreateOpts) (objects.Writer, error) {
	if free, err := repo.Free(nil); err == nil {
		if opts != nil && free < int64(opts.Alloc) {
			return nil, objectsmod.ErrNoSpaceLeft
		}
	}

	w, err := NewWriter(repo, repo.root)

	return w, err
}

func (repo *Repository) Free(ctx *astral.Context) (int64, error) {
	usage, err := DiskUsage(repo.root)
	if err != nil {
		return -1, errors.ErrUnsupported
	}

	return int64(usage.Free), nil
}

// Delete removes the object file. A partial ID is resolved and removed under one hold of mu.
func (repo *Repository) Delete(ctx *astral.Context, objectID *astral.ObjectID) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	var resolved = objectID
	if objectID.Size == 0 {
		found, err := repo.findPartialLocked(ctx, objectID.Hash)
		if err != nil {
			return err
		}
		resolved = found
	}

	return repo.removeObjectLocked(resolved)
}

// removeObjectLocked removes the file of a resolved full ID and its index entry. The caller holds mu.
func (repo *Repository) removeObjectLocked(objectID *astral.ObjectID) error {
	err := os.Remove(filepath.Join(repo.root, objectID.String()))
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	repo.evictLocked(objectID)

	// why: map a missing-file miss to ErrNotFound so purge skips this leaf instead of aborting the whole pass
	if err != nil {
		return objectsmod.ErrNotFound
	}
	return nil
}

func (repo *Repository) String() string {
	return repo.label
}

func (repo *Repository) pushAdded(id *astral.ObjectID) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	repo.addQueue = repo.addQueue.Push(id)
}

// added returns the queue element a new follower subscribes from.
func (repo *Repository) added() *sig.Queue[*astral.ObjectID] {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	return repo.addQueue
}
