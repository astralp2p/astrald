package fs

import (
	"context"
	"errors"
	"fmt"
	objectsmod "github.com/cryptopunkscc/astrald/mod/objects"
	"io"
	"os"
	"path/filepath"

	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/sig"
	"github.com/cryptopunkscc/astrald/lib/paths"
	"github.com/cryptopunkscc/astrald/mod/fs"
)

var _ objectsmod.Repository = &WatchRepository{}
var _ objectsmod.AfterRemovedCallback = &WatchRepository{}

// WatchRepository is a read-only, database-indexed repository that watches a directory tree for
// filesystem events and keeps the index up to date via the module's indexer.
// scanCancel stops any in-progress initial scan when the repository is removed.
type WatchRepository struct {
	mod        *Module
	label      string
	root       string
	watcher    *Watcher
	scanCancel context.CancelFunc
}

// NewWatchRepository creates a WatchRepository for an absolute directory path, wires inotify
// callbacks, and registers the root with the module's indexer for initial and incremental indexing.
// Returns an error if root is relative, does not exist, or is not a directory.
func NewWatchRepository(mod *Module, root string, label string) (repo *WatchRepository, err error) {
	if !filepath.IsAbs(root) {
		return nil, fs.ErrNotAbsolute
	}

	stat, err := os.Stat(root)
	switch {
	case err != nil:
		return nil, err
	case !stat.IsDir():
		return nil, fmt.Errorf("path %v is not a directory", root)
	}

	repo = &WatchRepository{
		mod:   mod,
		label: label,
		root:  root,
	}

	repo.watcher, err = NewWatcher()
	if err != nil {
		return nil, err
	}

	repo.watcher.OnRenamed = repo.onChange
	repo.watcher.OnWriteDone = repo.onChange
	repo.watcher.OnRemoved = repo.onRemove

	repo.watcher.OnDirCreated = func(s string) {
		repo.watcher.Add(s, true)
	}

	repo.watcher.Add(root, true)

	// indexer will know to scan this root while init
	err = repo.mod.indexer.addRoot(root)
	if err != nil {
		return nil, err
	}

	return
}

var _ objectsmod.Repository = &WatchRepository{}

// Contains checks the database index rather than the filesystem directly.
func (repo *WatchRepository) Contains(ctx *astral.Context, objectID *astral.ObjectID) (bool, error) {
	return repo.mod.db.ObjectExists(repo.root, objectID)
}

func (repo *WatchRepository) onChange(path string) {
	repo.mod.indexer.requeuePath(path)
}

func (repo *WatchRepository) onRemove(path string) {
	repo.mod.indexer.deletePath(path)
}

// Scan emits all known object IDs from the database, then — when follow is true — sends a nil
// sentinel before streaming live indexer events filtered to this repository's root.
func (repo *WatchRepository) Scan(ctx *astral.Context, follow bool) (<-chan *astral.ObjectID, error) {
	ch := make(chan *astral.ObjectID)

	go func() {
		defer close(ch)

		ids, err := repo.mod.db.UniqueObjectIDs(repo.root)
		if err != nil {
			repo.mod.log.Error("db error: %v", err)
			return
		}

		for _, id := range ids {
			select {
			case ch <- id:
			case <-ctx.Done():
				return
			}
		}

		if follow {
			subscribe := sig.Subscribe(ctx, repo.mod.indexer.subscribe())
			defer repo.mod.indexer.unsubscribe()

			select {
			case ch <- nil:
			case <-ctx.Done():
				return
			}

			for event := range subscribe {
				if paths.PathUnder(event.Path, repo.root, filepath.Separator) {
					select {
					case ch <- event.ObjectID:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch, nil
}

// Read resolves the object to a filesystem path via the database index and tries each candidate
// in order, returning the first file that opens and seeks successfully.
func (repo *WatchRepository) Read(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	rows, err := repo.mod.db.FindObject(repo.root, objectID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, objectsmod.ErrNotFound
	}
	if limit == 0 {
		limit = int64(objectID.Size)
	}

	for _, row := range rows {
		f, err := os.Open(row.Path)
		if err != nil {
			continue
		}

		if offset != 0 {
			pos, err := f.Seek(offset, io.SeekStart)
			if err != nil || pos != offset {
				f.Close()
				continue
			}
		}

		return NewReader(f, row.Path, limit, repo), nil
	}

	return nil, objectsmod.ErrNotFound
}

// Create is not supported; WatchRepository is read-only.
func (repo *WatchRepository) Create(ctx *astral.Context, opts *objectsmod.CreateOpts) (objects.Writer, error) {
	return nil, errors.ErrUnsupported
}

func (repo *WatchRepository) Label() string {
	return repo.label
}

// Delete is not supported; WatchRepository is read-only.
func (repo *WatchRepository) Delete(ctx *astral.Context, objectID *astral.ObjectID) error {
	return errors.ErrUnsupported
}

// Free always returns 0 because the repository does not allocate storage; writes are unsupported.
func (repo *WatchRepository) Free(ctx *astral.Context) (int64, error) {
	return 0, nil
}

func (repo *WatchRepository) String() string {
	return repo.label
}

// AfterRemoved is called by the objects system when this repository is unregistered.
// It cancels any in-flight scan, closes the filesystem watcher, and deregisters the root from the indexer.
func (repo *WatchRepository) AfterRemoved(name string) {
	if repo.scanCancel != nil {
		repo.scanCancel()
	}

	if err := repo.watcher.Close(); err != nil {
		repo.mod.log.Error("%v watcher close error: %v", name, err)
	}

	if err := repo.mod.indexer.removeRoot(repo.root); err != nil {
		repo.mod.log.Error("%v indexer DeletePath root error: %v", name, err)
	}
}
