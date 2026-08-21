package nodes

import (
	"errors"
	"io"
	"sync"

	"github.com/astralp2p/astrald/mod/nodes"
)

var _ io.ReadCloser = &muxSessionReader{}

// muxSessionReader wraps InputBuffer with blocking reads, pause/resume, and buffer
// swapping for migration. Queued next buffer takes over after the current one drains.
type muxSessionReader struct {
	cond       *sync.Cond
	paused     bool
	buf        *InputBuffer
	nextBuffer *InputBuffer
}

func newSessionReader(buf *InputBuffer) *muxSessionReader {
	r := &muxSessionReader{buf: buf}
	r.cond = sync.NewCond(&sync.Mutex{})
	return r
}

func (r *muxSessionReader) Buf() *InputBuffer {
	r.cond.L.Lock()
	defer r.cond.L.Unlock()
	return r.buf
}

func (r *muxSessionReader) Close() error {
	r.cond.L.Lock()
	buf := r.buf
	nextBuffer := r.nextBuffer
	r.nextBuffer = nil
	r.paused = false
	r.cond.Broadcast()
	r.cond.L.Unlock()

	if buf != nil {
		_ = buf.Close()
	}
	if nextBuffer != nil {
		_ = nextBuffer.Close()
	}

	return nil
}

// Push appends to the current buffer, falling through to the queued next buffer if the current one is already closed (mid-migration).
func (r *muxSessionReader) Push(p []byte) error {
	r.cond.L.Lock()
	buf := r.buf
	next := r.nextBuffer
	r.cond.L.Unlock()

	err := buf.Push(p)
	if err == nodes.ErrBufferClosed && next != nil {
		return next.Push(p)
	}
	return err
}

func (r *muxSessionReader) SetNextBuffer(buf *InputBuffer) {
	r.cond.L.Lock()
	defer r.cond.L.Unlock()
	r.nextBuffer = buf
}

func (r *muxSessionReader) Pause() {
	r.cond.L.Lock()
	defer r.cond.L.Unlock()
	r.paused = true
	r.cond.Broadcast()
}

func (r *muxSessionReader) Resume() {
	r.cond.L.Lock()
	defer r.cond.L.Unlock()
	r.paused = false
	r.cond.Broadcast()
}

// Advance closes the current buffer and, once it has drained, promotes the queued next buffer; no-op if none is queued.
func (r *muxSessionReader) Advance() {
	r.cond.L.Lock()
	defer r.cond.L.Unlock()
	if r.nextBuffer == nil {
		return
	}
	_ = r.buf.Close()
	if r.buf.IsEmpty() {
		r.buf = r.nextBuffer
		r.nextBuffer = nil
		r.cond.Broadcast()
	}
}

// Read blocks while paused or empty, transparently switching to the next buffer at EOF, so migration is invisible to the caller.
func (r *muxSessionReader) Read(p []byte) (n int, err error) {
	for {
		r.cond.L.Lock()
		for r.paused {
			r.cond.Wait()
		}
		n, err := r.buf.Read(p)
		switch {
		case err == nil:
			r.cond.L.Unlock()
			return n, nil
		case err == io.EOF:
			if r.nextBuffer != nil {
				r.buf = r.nextBuffer
				r.nextBuffer = nil
				r.cond.L.Unlock()
				continue
			}

			r.cond.L.Unlock()
			return n, err
		default:
			var emptyErr *ErrBufferEmpty
			if errors.As(err, &emptyErr) {
				r.cond.L.Unlock()
				<-emptyErr.Wait()
				continue
			}

			r.cond.L.Unlock()
			return n, err
		}
	}
}
