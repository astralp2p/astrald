package mem

import (
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"slices"
	"sync/atomic"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

var _ objectsmod.Repository = &Repository{}

const DefaultSize = 64 * 1024 * 1024 // 64MB

type Repository struct {
	objects  sig.Map[string, []byte]
	mod      objectsmod.Module
	used     atomic.Int64
	size     int64
	name     string
	addQueue *sig.Queue[*astral.ObjectID]
}

var _ objectsmod.Repository = &Repository{}

func New(name string, size int64) *Repository {
	var repo = &Repository{
		name:     name,
		size:     size,
		addQueue: &sig.Queue[*astral.ObjectID]{},
	}

	if len(repo.name) == 0 {
		repo.name = "Memory"
	}
	if repo.size == 0 {
		repo.size = DefaultSize
	}
	return repo
}

func (repo *Repository) Label() string {
	return repo.name
}

func (repo *Repository) Create(ctx *astral.Context, opts *objectsmod.CreateOpts) (objects.Writer, error) {
	if free, err := repo.Free(ctx); err == nil {
		if opts != nil && int64(opts.Alloc) > free {
			return nil, objectsmod.ErrNoSpaceLeft
		}
	}

	return NewWriter(repo), nil
}

func (repo *Repository) Contains(ctx *astral.Context, objectID *astral.ObjectID) (bool, error) {
	return slices.Contains(repo.objects.Keys(), objectID.String()), nil
}

// Read serves only the device zone; other zones are excluded.
func (repo *Repository) Read(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	if !ctx.Zone().Is(astral.ZoneDevice) {
		return nil, astral.ErrZoneExcluded
	}

	s, e, err := getSliceBounds(objectID, offset, limit)
	if err != nil {
		return nil, err
	}

	bytes, found := repo.objects.Get(objectID.String())
	if !found {
		return nil, objectsmod.ErrNotFound
	}

	return NewReader(bytes[s:e], repo), nil
}

// Scan streams existing object IDs, then closes unless follow is set.
// When following, a nil sentinel separates the initial set from live additions.
func (repo *Repository) Scan(ctx *astral.Context, follow bool) (<-chan *astral.ObjectID, error) {
	ch := make(chan *astral.ObjectID)

	var s <-chan *astral.ObjectID

	go func() {
		defer close(ch)

		if follow {
			s = sig.Subscribe(ctx, repo.addQueue)
		}

		for _, s := range repo.objects.Keys() {
			id, err := astral.ParseID(s)
			if err != nil {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case ch <- id:
			}
		}

		if s != nil {
			select {
			case <-ctx.Done():
				return
			case ch <- nil:
			}

			for i := range s {
				select {
				case <-ctx.Done():
					return
				case ch <- i:
				}
			}
		}
	}()

	return ch, nil
}

func (repo *Repository) Delete(ctx *astral.Context, objectID *astral.ObjectID) error {
	_, ok := repo.objects.Delete(objectID.String())
	if !ok {
		return objectsmod.ErrNotFound
	}
	return nil
}

func (repo *Repository) Used() int64 {
	return repo.used.Load()
}

func (repo *Repository) Free(ctx *astral.Context) (int64, error) {
	return repo.size - repo.used.Load(), nil
}

func (repo *Repository) String() string {
	return repo.name
}

func (repo *Repository) free() int64 {
	return repo.size - repo.used.Load()
}

func (repo *Repository) pushAdded(id *astral.ObjectID) {
	repo.addQueue = repo.addQueue.Push(id)
}

func getSliceBounds(objectID *astral.ObjectID, offset int64, limit int64) (s int, e int, err error) {
	switch {
	case offset < 0 || offset > int64(objectID.Size):
		return 0, 0, objectsmod.ErrOutOfBounds
	case limit < 0:
		return 0, 0, objectsmod.ErrOutOfBounds
	case limit == 0:
		limit = int64(objectID.Size)
	}

	s = int(offset)
	e = min(s+int(limit), int(objectID.Size))

	return
}
