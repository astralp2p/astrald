package fs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/lib/paths"
	"github.com/astralp2p/astrald/mod/fs"
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

	// why: the indexer stores paths under the cleaned root.
	// why: PathUnder matches root plus a separator, so a trailing separator would exclude every row.
	root = filepath.Clean(root)

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
// A partial ID also checks the file of each candidate row.
func (repo *WatchRepository) Contains(ctx *astral.Context, objectID *astral.ObjectID) (bool, error) {
	if objectID.Size != 0 {
		return repo.mod.db.ObjectExists(repo.root, objectID)
	}

	_, err := repo.findPartial(ctx, objectID.Hash)
	switch {
	case errors.Is(err, objectsmod.ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

// findObject returns the full ID of the indexed object objectID names.
// A full ID is returned as a copy; Read checks its rows.
func (repo *WatchRepository) findObject(ctx *astral.Context, objectID *astral.ObjectID) (*astral.ObjectID, error) {
	if objectID.Size != 0 {
		var id = *objectID
		return &id, nil
	}

	return repo.findPartial(ctx, objectID.Hash)
}

// findPartial returns the one indexed full ID with the hash that has an in-root regular file of its recorded size.
// A row whose file cannot be checked is skipped; its error is returned only when no row resolves.
// why: the query matches a text tail of the hash, so the hash is compared exactly here.
func (repo *WatchRepository) findPartial(ctx *astral.Context, hash [32]byte) (*astral.ObjectID, error) {
	rows, err := repo.mod.db.FindByHashTail(ctx, repo.root, hashTail(hash))
	if err != nil {
		return nil, err
	}

	var found *astral.ObjectID
	var failed error
	for _, row := range rows {
		if row.DataID == nil || row.DataID.Hash != hash {
			continue
		}
		if found != nil && found.IsEqual(row.DataID) {
			continue
		}

		ok, err := repo.isRecordedFile(row)
		switch {
		case err != nil:
			failed = cmp.Or(failed, err)
			continue
		case !ok:
			continue
		case found != nil:
			return nil, objectsmod.ErrAmbiguousObjectID
		}
		found = row.DataID
	}

	switch {
	case found != nil:
		return found, nil
	case failed != nil:
		return nil, failed
	}

	return nil, objectsmod.ErrNotFound
}

// isRecordedFile reports whether an index row's path lies under the root and holds a regular file of the recorded size.
// A missing file is not an error.
// note: an equal length does not verify the file's hash.
func (repo *WatchRepository) isRecordedFile(row *dbLocalFile) (bool, error) {
	if !paths.PathUnder(row.Path, repo.root, filepath.Separator) {
		return false, nil
	}

	stat, err := os.Stat(row.Path)
	switch {
	// why: stat reports ENOTDIR for a path whose parent directory became a regular file, which is a missing path.
	case errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
		return false, nil
	case err != nil:
		return false, err
	}

	return stat.Mode().IsRegular() && uint64(stat.Size()) == row.DataID.Size, nil
}

// hashTail returns the end that every data1 string of the hash shares, whatever the size.
// why: characters 0 to 12 of the unstripped encoding of Size||Hash carry Size bits, and PartialString keeps characters 12 to 63.
// why: String strips leading 'y', and for Size 0 the strip reaches past character 12.
func hashTail(hash [32]byte) string {
	var partial = strings.TrimPrefix(astral.ObjectID{Hash: hash}.PartialString(), "data0")

	return strings.TrimLeft(partial[1:], "y")
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
// in order, returning the first in-root regular file of the object's size.
func (repo *WatchRepository) Read(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	resolved, err := repo.findObject(ctx, objectID)
	if err != nil {
		return nil, err
	}

	rows, err := repo.mod.db.FindObject(repo.root, resolved)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		f := repo.openRow(row)
		if f == nil {
			continue
		}

		n, err := objectsmod.ResolveReadLimit(row.DataID, offset, limit)
		if err != nil {
			f.Close()
			return nil, err
		}

		if offset != 0 {
			pos, err := f.Seek(offset, io.SeekStart)
			if err != nil || pos != offset {
				f.Close()
				continue
			}
		}

		return NewReader(f, row.DataID, n, repo), nil
	}

	return nil, objectsmod.ErrNotFound
}

// openRow opens the file of an index row when it lies under the root and is a regular file of the recorded size.
// openRow returns nil for any other row.
func (repo *WatchRepository) openRow(row *dbLocalFile) *os.File {
	if !paths.PathUnder(row.Path, repo.root, filepath.Separator) {
		return nil
	}

	f, err := os.Open(row.Path)
	if err != nil {
		return nil
	}

	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || uint64(stat.Size()) != row.DataID.Size {
		f.Close()
		return nil
	}

	return f
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
