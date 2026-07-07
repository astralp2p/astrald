package fs

import (
	"errors"
	objectsmod "github.com/cryptopunkscc/astrald/mod/objects"
	"io"
	"os"
	"path/filepath"

	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/sig"
)

var _ objectsmod.Repository = &Repository{}

// Repository is a filesystem-backed object store rooted at a single directory.
// addQueue broadcasts newly committed object IDs to active Scan followers.
type Repository struct {
	mod      *Module
	label    string
	root     string
	addQueue *sig.Queue[*astral.ObjectID]
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
			subscribe = sig.Subscribe(ctx, repo.addQueue)
		}

		entries, err := os.ReadDir(repo.root)
		if err != nil {
			repo.mod.log.Error("cannot read dir %v: %v", repo.root, err)
			return
		}

		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				continue
			}

			objectID, err := astral.ParseID(entry.Name())
			if err != nil {
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

	if limit == 0 {
		limit = int64(objectID.Size)
	}

	path := filepath.Join(repo.root, objectID.String())

	f, err := os.Open(path)
	if err != nil {
		return nil, objectsmod.ErrNotFound
	}

	if offset != 0 {
		pos, err := f.Seek(offset, io.SeekStart)
		if err != nil {
			f.Close()
			return nil, objectsmod.ErrNotFound
		}

		if pos != offset {
			f.Close()
			return nil, objectsmod.ErrNotFound
		}
	}

	return NewReader(f, path, limit, repo), nil

}

func (repo *Repository) Contains(ctx *astral.Context, objectID *astral.ObjectID) (bool, error) {
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

func (repo *Repository) Delete(ctx *astral.Context, objectID *astral.ObjectID) error {
	path := filepath.Join(repo.root, objectID.String())
	err := os.Remove(path)
	// why: map a missing-file miss to ErrNotFound so purge skips this leaf instead of aborting the whole pass
	if os.IsNotExist(err) {
		return objectsmod.ErrNotFound
	}
	return err
}

func (repo *Repository) String() string {
	return repo.label
}

func (repo *Repository) pushAdded(id *astral.ObjectID) {
	repo.addQueue = repo.addQueue.Push(id)
}
