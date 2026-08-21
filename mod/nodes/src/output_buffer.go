package nodes

import (
	"io"
	"sync"

	"github.com/astralp2p/astrald/mod/nodes"
)

var _ io.WriteCloser = &OutputBuffer{}

// OutputBuffer is a flow-controlled send buffer. Write consumes available space and
// delegates to the write callback; returns ErrBufferEmpty when full. Grow extends
// available space.
type OutputBuffer struct {
	mu     sync.Mutex
	wsize  int
	ready  chan struct{}
	closed bool

	write func([]byte) error
}

func NewOutputBuffer(write func([]byte) error) *OutputBuffer {
	return &OutputBuffer{write: write}
}

// Write never blocks: with no available space it returns *ErrBufferEmpty whose channel
// closes once Grow adds space. It may consume only part of p, bounded by available space.
func (b *OutputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return 0, nodes.ErrBufferClosed
	}
	if b.wsize == 0 {
		ch := b.readyCh()
		b.mu.Unlock()
		return 0, &ErrBufferEmpty{ch: ch}
	}
	n := min(b.wsize, len(p))
	b.wsize -= n
	b.mu.Unlock()

	return n, b.write(p[:n])
}

func (b *OutputBuffer) Grow(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wsize += n
	b.signal()
}

// Close stops writes immediately. Nothing to drain: every Write already dispatched bytes via the write callback.
func (b *OutputBuffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.signal() // wake blocked writer so it surfaces ErrBufferClosed instead of sleeping

	return nil
}

func (b *OutputBuffer) readyCh() chan struct{} {
	if b.ready == nil {
		b.ready = make(chan struct{})
	}
	return b.ready
}

func (b *OutputBuffer) signal() {
	if b.ready != nil {
		close(b.ready)
		b.ready = nil
	}
}
