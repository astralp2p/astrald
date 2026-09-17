package nodes

import (
	"errors"
	"sync"
	"testing"

	"github.com/astralp2p/astrald/mod/nodes"
)

type writeRecorder struct {
	mu      sync.Mutex
	written []byte
	err     error
}

func (r *writeRecorder) write(p []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.written = append(r.written, p...)
	return r.err
}

func (r *writeRecorder) recorded() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.written)
}

func TestOutputBufferWriteNeedsCredit(t *testing.T) {
	rec := &writeRecorder{}
	b := NewOutputBuffer(rec.write)

	n, err := b.Write([]byte("abc"))
	var empty *ErrBufferEmpty
	if n != 0 || !errors.As(err, &empty) {
		t.Fatalf("Write without credit = (%d, %v); want (0, *ErrBufferEmpty)", n, err)
	}
	if isClosed(empty.Wait()) {
		t.Fatal("Wait() channel closed before Grow; want open")
	}

	b.Grow(2)
	if !isClosed(empty.Wait()) {
		t.Fatal("Wait() channel open after Grow; want closed")
	}

	n, err = b.Write([]byte("abc"))
	if n != 2 || err != nil {
		t.Fatalf("Write with 2 bytes of credit = (%d, %v); want (2, nil)", n, err)
	}
	if got := rec.recorded(); got != "ab" {
		t.Fatalf("write callback got %q; want %q", got, "ab")
	}
}

func TestOutputBufferWriteReturnsCallbackError(t *testing.T) {
	errX := errors.New("link write failed")
	b := NewOutputBuffer((&writeRecorder{err: errX}).write)
	b.Grow(3)

	n, err := b.Write([]byte("abc"))
	if n != 3 || !errors.Is(err, errX) {
		t.Fatalf("Write = (%d, %v); want (3, %v)", n, err, errX)
	}
}

func TestOutputBufferCloseStopsWrites(t *testing.T) {
	b := NewOutputBuffer((&writeRecorder{}).write)

	_, err := b.Write([]byte("abc"))
	var empty *ErrBufferEmpty
	if !errors.As(err, &empty) {
		t.Fatalf("Write without credit = %v; want *ErrBufferEmpty", err)
	}

	if err := b.Close(); err != nil {
		t.Fatalf("Close() = %v; want nil", err)
	}
	if !isClosed(empty.Wait()) {
		t.Fatal("pending Wait() channel open after Close; want closed")
	}

	n, err := b.Write([]byte("abc"))
	if n != 0 || !errors.Is(err, nodes.ErrBufferClosed) {
		t.Fatalf("Write after Close = (%d, %v); want (0, %v)", n, err, nodes.ErrBufferClosed)
	}
}
