package objects

import (
	"errors"
	"github.com/astralp2p/astral-go/astral"
	"io"
	"sync"
)

// ReadSeeker adapts a repository's offset-based Read into an io.ReadSeeker.
// note: Seek reopens the underlying reader at the new position rather than seeking in place.
type ReadSeeker struct {
	readerID *astral.Identity
	objectID *astral.ObjectID
	repo     Repository
	r        io.ReadCloser
	mu       sync.Mutex
	zone     astral.Zone
	pos      int64
}

func NewReadSeeker(ctx *astral.Context, objectID *astral.ObjectID, repo Repository, r io.ReadCloser) *ReadSeeker {
	return &ReadSeeker{
		readerID: ctx.Identity(),
		zone:     ctx.Zone(),
		objectID: objectID,
		repo:     repo,
		r:        r,
	}
}

func (r *ReadSeeker) Read(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.r == nil {
		err = r.openAt(0)
		if err != nil {
			return
		}
	}

	n, err = r.r.Read(p)
	r.pos += int64(n)
	return n, err
}

// Seek reopens the underlying reader at the resolved position; it does not validate against object bounds.
func (r *ReadSeeker) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var pos int64

	switch whence {
	case io.SeekStart:
		pos = offset
	case io.SeekCurrent:
		pos = int64(r.pos) + offset
	case io.SeekEnd:
		pos = int64(r.objectID.Size) + offset
	}

	err := r.openAt(pos)
	r.pos = pos

	return r.pos, err
}

func (r *ReadSeeker) Close() error {
	if c, ok := r.r.(io.Closer); ok {
		return c.Close()
	}
	return errors.New("reader does not implement io.Closer")
}

func (r *ReadSeeker) openAt(pos int64) (err error) {
	if r.r != nil {
		r.r.Close()
	}

	ctx := astral.NewContext(nil).WithZone(r.zone).WithIdentity(r.readerID)

	r.r, err = r.repo.Read(ctx, r.objectID, pos, 0)
	r.pos = pos

	return
}
