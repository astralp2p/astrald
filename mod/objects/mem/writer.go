package mem

import (
	"bytes"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
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
		return 0, objectsmod.ErrClosedPipe
	}

	if !w.reserve(int64(len(p))) {
		return 0, objectsmod.ErrNoSpaceLeft
	}

	n, err = w.buf.Write(p)
	if n < len(p) {
		w.release(int64(len(p) - n))
	}

	return n, err
}

// Commit resolves the buffered bytes to an object ID and stores them.
// Idempotent: only the first call succeeds; later calls return ErrClosedPipe.
func (w *Writer) Commit() (*astral.ObjectID, error) {
	if !w.closed.CompareAndSwap(false, true) {
		return nil, objectsmod.ErrClosedPipe
	}

	var buf = w.buf.Bytes()
	var objectID, _ = astral.Resolve(bytes.NewReader(buf))
	w.buf = nil

	// why: Set keeps the entry already stored, so a duplicate holds no bytes and releases its reservation.
	if _, stored := w.objects.Set(objectID.Hash, buf); !stored {
		w.release(int64(len(buf)))
		return objectID, nil
	}

	w.Repository.pushAdded(objectID)

	return objectID, nil
}

func (w *Writer) Discard() error {
	if w.closed.CompareAndSwap(false, true) {
		w.release(int64(w.buf.Len()))
		w.buf = nil
	}
	return nil
}
