package objects

import (
	"errors"
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
	"io"
)

// NilRepository is a Repository stub whose every operation returns ErrUnsupported.
type NilRepository struct {
}

func (n NilRepository) Create(ctx *astral.Context, opts *CreateOpts) (objects.Writer, error) {
	return nil, errors.ErrUnsupported
}

func (n NilRepository) Contains(ctx *astral.Context, objectID *astral.ObjectID) (bool, error) {
	return false, errors.ErrUnsupported
}

func (n NilRepository) Scan(ctx *astral.Context, follow bool) (<-chan *astral.ObjectID, error) {
	return nil, errors.ErrUnsupported
}

func (n NilRepository) Delete(ctx *astral.Context, objectID *astral.ObjectID) error {
	return errors.ErrUnsupported
}

func (n NilRepository) Read(ctx *astral.Context, objectID *astral.ObjectID, offset int64, limit int64) (io.ReadCloser, error) {
	return nil, errors.ErrUnsupported
}

func (n NilRepository) Free(ctx *astral.Context) (int64, error) {
	return 0, errors.ErrUnsupported
}
