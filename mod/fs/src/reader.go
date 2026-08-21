package fs

import (
	"io"
	"os"

	"github.com/astralp2p/astrald/mod/objects"
)

// Reader wraps a file with an optional byte limit and a back-reference to the owning repository.
// A limit of -1 disables enforcement; 0 signals EOF immediately.
type Reader struct {
	io.ReadSeekCloser
	name  string
	limit int64
	repo  objects.Repository
}

// NewReader returns a new file reader with a limit on the amount of bytes that can be read. -1 means no limit.
func NewReader(f *os.File, name string, limit int64, repo objects.Repository) *Reader {
	return &Reader{
		ReadSeekCloser: f,
		name:           name,
		limit:          limit,
		repo:           repo,
	}
}

// Read enforces the remaining byte limit, reducing it with each successful read.
func (r *Reader) Read(p []byte) (n int, err error) {
	switch {
	case r.limit < 0:
		return r.ReadSeekCloser.Read(p)
	case r.limit == 0:
		return 0, io.EOF
	}

	l := min(len(p), int(r.limit))
	n, err = r.ReadSeekCloser.Read(p[:l])
	r.limit -= int64(n)

	return n, err
}

func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	return r.ReadSeekCloser.Seek(offset, whence)
}

func (r *Reader) Close() error {
	return r.ReadSeekCloser.Close()
}

func (r *Reader) Repo() objects.Repository {
	return r.repo
}
