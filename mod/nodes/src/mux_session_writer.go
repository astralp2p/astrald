package nodes

import (
	"errors"
	"io"
	"sync"
)

var _ io.WriteCloser = &muxSessionWriter{}

// muxSessionWriter wraps OutputBuffer with blocking writes, pause/resume, and buffer
// hot-swapping for migration. SwapBuf replaces the buffer atomically.
type muxSessionWriter struct {
	cond     *sync.Cond
	closed   bool
	paused   bool
	inflight int // writes into buf in progress, outside cond.L
	buf      *OutputBuffer
	reset    func()     // sends Reset frame on current link
	mu       sync.Mutex // serializes Write
}

func newSessionWriter(buf *OutputBuffer, reset func()) *muxSessionWriter {
	w := &muxSessionWriter{buf: buf, reset: reset}
	w.cond = sync.NewCond(&sync.Mutex{})
	return w
}

func (w *muxSessionWriter) Buf() *OutputBuffer {
	w.cond.L.Lock()
	defer w.cond.L.Unlock()
	return w.buf
}

// SwapBuf installs a new buffer and reset func for the new link, closing the old buffer.
func (w *muxSessionWriter) SwapBuf(buf *OutputBuffer, reset func()) {
	w.cond.L.Lock()
	old := w.buf
	w.buf = buf
	w.reset = reset
	w.cond.L.Unlock()
	if old != nil {
		old.Close()
	}
}

// Close sends a Reset frame to the peer and closes the buffer; idempotent. Use PeerClose to skip the Reset.
func (w *muxSessionWriter) Close() error {
	w.cond.L.Lock()
	if w.closed {
		w.cond.L.Unlock()
		return nil
	}
	w.closed = true
	reset := w.reset
	buf := w.buf
	w.cond.Broadcast()
	w.cond.L.Unlock()
	if reset != nil {
		reset()
	}
	buf.Close()
	return nil
}

// PeerClose closes the writer without sending a Reset frame.
func (w *muxSessionWriter) PeerClose() error {
	w.cond.L.Lock()
	if w.closed {
		w.cond.L.Unlock()
		return nil
	}
	w.closed = true
	buf := w.buf
	w.cond.Broadcast()
	w.cond.L.Unlock()
	buf.Close()
	return nil
}

func (w *muxSessionWriter) Grow(n int) {
	w.cond.L.Lock()
	buf := w.buf
	w.cond.L.Unlock()
	buf.Grow(n)
}

// Pause stops new writes and returns once no write is in progress, so a
// migration that follows sends nothing on the old link behind its frame.
func (w *muxSessionWriter) Pause() {
	w.cond.L.Lock()
	defer w.cond.L.Unlock()
	w.paused = true
	for w.inflight > 0 {
		w.cond.Wait()
	}
}

func (w *muxSessionWriter) Resume() {
	w.cond.L.Lock()
	defer w.cond.L.Unlock()
	w.paused = false
	w.cond.Broadcast()
}

// Write blocks while paused and retries when the buffer runs out of flow-control credit, until all of p is written or the writer closes.
//
// why the buffer is written outside cond.L: the write blocks on the link
// while the link's transport is full (a kcp window), and the link's read loop
// takes cond.L in Grow and PeerClose. Holding it across the write stopped
// this node reading the link, and two nodes in that state stalled the link
// until the write watchdog closed it. Pause waits for inflight instead.
func (w *muxSessionWriter) Write(p []byte) (int, error) {
	// why mu: writes of one session stay in order, as they did under cond.L.
	w.mu.Lock()
	defer w.mu.Unlock()

	total := 0
	for len(p) > 0 {
		buf, err := w.begin()
		if err != nil {
			return total, err
		}

		n, err := buf.Write(p)
		w.end()
		if err != nil {
			var emptyErr *ErrBufferEmpty
			if errors.As(err, &emptyErr) {
				<-emptyErr.Wait()
				continue
			}
			return total, err
		}

		total += n
		p = p[n:]
	}

	return total, nil
}

// begin waits while paused and counts a write into the current buffer in progress.
func (w *muxSessionWriter) begin() (*OutputBuffer, error) {
	w.cond.L.Lock()
	defer w.cond.L.Unlock()

	for w.paused && !w.closed {
		w.cond.Wait()
	}
	if w.closed {
		return nil, io.ErrClosedPipe
	}
	w.inflight++
	return w.buf, nil
}

// end ends a write that begin counted and wakes a Pause waiting for it.
func (w *muxSessionWriter) end() {
	w.cond.L.Lock()
	defer w.cond.L.Unlock()

	w.inflight--
	w.cond.Broadcast()
}
