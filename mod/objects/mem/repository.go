package mem

import (
	"errors"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"sync"
	"sync/atomic"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

var _ objectsmod.Repository = &Repository{}

const DefaultSize = 64 * 1024 * 1024 // 64MB

type Repository struct {
	objects  sig.Map[[32]byte, []byte] // stored bytes by the hash of their content
	mod      objectsmod.Module
	used     atomic.Int64
	size     int64
	name     string
	mu       sync.Mutex // guards addQueue
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

// storedObject is an object the repository holds, with its full ID.
type storedObject struct {
	id   astral.ObjectID
	data []byte
}

// findObject returns the stored object objectID names, by hash alone for a partial ID.
// A full ID matches only a stored object of its size.
func (repo *Repository) findObject(objectID *astral.ObjectID) (*storedObject, error) {
	data, found := repo.objects.Get(objectID.Hash)
	if !found {
		return nil, objectsmod.ErrNotFound
	}

	var object = &storedObject{
		id:   astral.ObjectID{Size: uint64(len(data)), Hash: objectID.Hash},
		data: data,
	}

	// why: Size 0 marks a partial ID, which carries no size to compare.
	if objectID.Size != 0 && objectID.Size != object.id.Size {
		return nil, objectsmod.ErrNotFound
	}

	return object, nil
}

func (repo *Repository) Contains(ctx *astral.Context, objectID *astral.ObjectID) (bool, error) {
	_, err := repo.findObject(objectID)
	switch {
	case errors.Is(err, objectsmod.ErrNotFound):
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

// Read serves only the device zone; other zones are excluded.
func (repo *Repository) Read(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (objectsmod.Reader, error) {
	if !ctx.Zone().Is(astral.ZoneDevice) {
		return nil, astral.ErrZoneExcluded
	}

	object, err := repo.findObject(objectID)
	if err != nil {
		return nil, err
	}

	n, err := objectsmod.ResolveReadLimit(&object.id, offset, limit)
	if err != nil {
		return nil, err
	}

	return NewReader(object.data[offset:offset+n], &object.id, repo), nil
}

// Scan streams existing object IDs, then closes unless follow is set.
// When following, a nil sentinel separates the initial set from live additions.
func (repo *Repository) Scan(ctx *astral.Context, follow bool) (<-chan *astral.ObjectID, error) {
	ch := make(chan *astral.ObjectID)

	var s <-chan *astral.ObjectID

	go func() {
		defer close(ch)

		if follow {
			s = sig.Subscribe(ctx, repo.added())
		}

		for hash, data := range repo.objects.Clone() {
			id := &astral.ObjectID{Size: uint64(len(data)), Hash: hash}
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
	object, err := repo.findObject(objectID)
	if err != nil {
		return err
	}

	// why: one hash names one content, so an entry stored again after the lookup is the same object.
	// why: Delete reports ok to a single caller, so the release runs once per stored object.
	old, ok := repo.objects.Delete(object.id.Hash)
	if !ok {
		return objectsmod.ErrNotFound
	}

	repo.release(int64(len(old)))

	return nil
}

// reserve claims n bytes of quota. reserve returns false when the claim exceeds the repository size.
func (repo *Repository) reserve(n int64) bool {
	for {
		var used = repo.used.Load()
		if used+n > repo.size {
			return false
		}
		if repo.used.CompareAndSwap(used, used+n) {
			return true
		}
	}
}

// release returns n bytes of quota to the repository.
func (repo *Repository) release(n int64) {
	repo.used.Add(-n)
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
