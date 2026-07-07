package mem

import (
	"bytes"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/objects"
	"sync/atomic"
)

var _ objects.Writer = &Writer{}

type Writer struct {
	*Repository
	buf    *bytes.Buffer
	closed atomic.Bool
}

func NewWriter(memStore *Repository) *Writer {
	return &Writer{
		Repository: memStore,
		buf:        &bytes.Buffer{},
	}
}

func (w *Writer) Write(p []byte) (n int, err error) {
	if w.closed.Load() {
		return 0, objects.ErrClosedPipe
	}

	if int64(len(p)) > w.free() {
		return 0, objects.ErrNoSpaceLeft
	}
	n, err = w.buf.Write(p)
	w.used.Add(int64(n))
	return n, err
}

// Commit resolves the buffered bytes to an object ID and stores them.
// Idempotent: only the first call succeeds; later calls return ErrClosedPipe.
func (w *Writer) Commit() (*astral.ObjectID, error) {
	if !w.closed.CompareAndSwap(false, true) {
		return nil, objects.ErrClosedPipe
	}

	var buf = w.buf.Bytes()
	var objectID, _ = astral.Resolve(bytes.NewReader(buf))

	w.objects.Set(objectID.String(), buf)

	w.Repository.pushAdded(objectID)

	return objectID, nil
}

func (w *Writer) Discard() error {
	if w.closed.CompareAndSwap(false, true) {
		w.used.Add(int64(-w.buf.Len())) // free up space
		w.buf = nil
	}
	return nil
}
