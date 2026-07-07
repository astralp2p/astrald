package objects

import (
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
)

// Repository is an interface for creating new data objects in storage
type Repository interface {
	// Label returns repository label
	Label() string

	// Create creates an object in the repository.
	Create(ctx *astral.Context, opts *CreateOpts) (objects.Writer, error)

	// Contains checks if the repository contains the specified object.
	Contains(ctx *astral.Context, objectID *astral.ObjectID) (bool, error)

	// Scan returns a channel of objects stored in the repository.
	// When follow is true, repositories must emit exactly one nil after the
	// current snapshot and before any live updates.
	Scan(ctx *astral.Context, follow bool) (<-chan *astral.ObjectID, error)

	// Delete deletes an object
	Delete(ctx *astral.Context, objectID *astral.ObjectID) error

	// Read reads raw object data
	Read(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (Reader, error)

	// Free returns available free space in the repository. -1 if unknown.
	Free(ctx *astral.Context) (int64, error)
}

type RepoGroup interface {
	Repository
	Add(repo string) error
	Remove(repo string) error
	List() []string
}

// AfterRemovedCallback can be optionally implemented by Repositories to be notified about removal
type AfterRemovedCallback interface {
	// AfterRemoved is called after the repo is removed with the name it was added under
	AfterRemoved(name string)
}

type CreateOpts struct {
	// Optional. Pre-allocate this much storage.
	Alloc int
}
