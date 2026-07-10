package mem

import (
	"bytes"
	"errors"
	"os"
	"sync/atomic"

	"github.com/astralp2p/astrald/mod/objects"
)

type Reader struct {
	r      *bytes.Reader
	bytes  []byte
	repo   objects.Repository
	closed atomic.Bool
}

var _ objects.Reader = &Reader{}

func NewReader(buf []byte, repo objects.Repository) *Reader {
	return &Reader{
		r:     bytes.NewReader(buf),
		bytes: buf,
		repo:  repo,
	}
}

func (r *Reader) Read(p []byte) (n int, err error) {
	if r.closed.Load() {
		return 0, os.ErrClosed
	}

	return r.r.Read(p)
}

// Seek is unsupported; this reader is forward-only.
func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	return 0, errors.ErrUnsupported
}

func (r *Reader) Close() error {
	r.closed.Store(true)
	return nil
}

func (r *Reader) Repo() objects.Repository {
	return r.repo
}
