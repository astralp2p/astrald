package nodes

import (
	"io"
	"sync"

	"github.com/astralp2p/astrald/mod/nodes"
)

var _ io.ReadCloser = &InputBuffer{}

// InputBuffer is a bounded receive buffer. Push appends whole chunks; Read
// consumes from the front. Non-blocking: when empty, Read returns ErrBufferEmpty
// with a one-shot wake channel — caller blocks on it, signal() closes and nils
// it so the next caller gets a fresh one. Closed buffer drains before EOF.
//
// Invariant: Push rejects if used+len(p) > size; no partial pushes.
type InputBuffer struct {
	mu      sync.Mutex
	size    int
	used    int
	buf     [][]byte      // chunk queue; front chunk may be partially consumed (trimmed in place, no copy)
	ready   chan struct{} // one-shot wake channel: nil → created by readyCh() → closed+nilified by signal()
	shut    chan struct{} // closed by Close(); onRead nilified at the same time to suppress post-close credit frames
	done    chan struct{} // closed when shut AND buf is empty
	drained bool
	closed  bool
	onRead  func(int) // called after each Read with bytes consumed; mux uses this to send a Read frame granting flow-control credit to the sender
}

func NewInputBuffer(size int, onRead func(int)) *InputBuffer {
	return &InputBuffer{size: size, onRead: onRead, shut: make(chan struct{}), done: make(chan struct{})}
}

// Push appends a whole chunk. All-or-nothing: rejects with ErrBufferOverflow rather than storing a partial chunk.
func (b *InputBuffer) Push(p []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nodes.ErrBufferClosed
	}
	if b.used+len(p) > b.size {
		return nodes.ErrBufferOverflow
	}

	b.buf = append(b.buf, p)
	b.used += len(p)
	b.signal()
	return nil
}

// Read never blocks: on an empty open buffer it returns *ErrBufferEmpty carrying a wake channel; on an empty closed buffer it returns io.EOF.
func (b *InputBuffer) Read(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.buf) == 0 {
		if b.closed {
			return 0, io.EOF
		}

		return 0, &ErrBufferEmpty{ch: b.readyCh()}
	}

	chunk := b.buf[0]
	n = min(len(p), len(chunk))
	copy(p, chunk[:n])
	chunk = chunk[n:]
	b.used -= n
	if len(chunk) > 0 {
		b.buf[0] = chunk
	} else {
		b.buf = b.buf[1:]
	}

	if b.onRead != nil {
		b.onRead(n)
	}
	b.checkDone()
	return
}

// Close signals no more pushes will arrive. Already-buffered data is still readable; EOF only after drained.
func (b *InputBuffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	b.onRead = nil // suppress flow-control credit frames after close
	close(b.shut)
	b.signal()    // wake blocked reader; it drains remaining data before returning EOF
	b.checkDone() // close done immediately if already empty

	return nil
}

func (b *InputBuffer) IsEmpty() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used == 0
}

// Closed is closed when Close is called, before remaining data is drained.
func (b *InputBuffer) Closed() <-chan struct{} {
	return b.shut
}

// Done is closed once the buffer is both closed and fully drained.
func (b *InputBuffer) Done() <-chan struct{} {
	return b.done
}

func (b *InputBuffer) checkDone() {
	if !b.drained && b.closed && len(b.buf) == 0 {
		b.drained = true
		close(b.done)
	}
}

func (b *InputBuffer) readyCh() chan struct{} {
	if b.ready == nil {
		b.ready = make(chan struct{})
	}
	return b.ready
}

func (b *InputBuffer) signal() {
	if b.ready != nil {
		close(b.ready)
		b.ready = nil
	}
}
