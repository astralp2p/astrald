package nodes

import (
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astrald/mod/nodes"
)

const bufferWaitTimeout = time.Second

// note: a call still pending after blockWindow counts as blocked.
const blockWindow = 30 * time.Millisecond

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func waitClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	case <-time.After(bufferWaitTimeout):
		return false
	}
}

type readRecorder struct {
	mu     sync.Mutex
	counts []int
}

func (r *readRecorder) onRead(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counts = append(r.counts, n)
}

func (r *readRecorder) recorded() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.counts...)
}

func readEmpty(t *testing.T, b *InputBuffer) *ErrBufferEmpty {
	t.Helper()
	n, err := b.Read(make([]byte, 8))
	var empty *ErrBufferEmpty
	if n != 0 || !errors.As(err, &empty) {
		t.Fatalf("Read on an empty open buffer = (%d, %v); want (0, *ErrBufferEmpty)", n, err)
	}
	return empty
}

func TestInputBufferPushIsAllOrNothing(t *testing.T) {
	b := NewInputBuffer(10, nil)

	if err := b.Push([]byte("abc")); err != nil {
		t.Fatalf("Push(abc) = %v; want nil", err)
	}
	if err := b.Push([]byte("12345678")); !errors.Is(err, nodes.ErrBufferOverflow) {
		t.Fatalf("Push of 8 bytes into 7 free = %v; want %v", err, nodes.ErrBufferOverflow)
	}

	p := make([]byte, 16)
	n, err := b.Read(p)
	if err != nil || string(p[:n]) != "abc" {
		t.Fatalf("Read = (%q, %v); want (\"abc\", nil)", p[:n], err)
	}
	if !b.IsEmpty() {
		t.Fatal("IsEmpty() = false after reading the only stored chunk; want true")
	}
}

func TestInputBufferReadTrimsPartialChunk(t *testing.T) {
	rec := &readRecorder{}
	b := NewInputBuffer(10, rec.onRead)
	if err := b.Push([]byte("abc")); err != nil {
		t.Fatalf("Push(abc) = %v; want nil", err)
	}

	reads := []string{"ab", "c"}
	for _, want := range reads {
		p := make([]byte, 2)
		n, err := b.Read(p)
		if err != nil || string(p[:n]) != want {
			t.Fatalf("Read = (%q, %v); want (%q, nil)", p[:n], err, want)
		}
	}

	if got, want := rec.recorded(), []int{2, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("onRead counts = %v; want %v", got, want)
	}
	if !b.IsEmpty() {
		t.Fatal("IsEmpty() = false after reading every byte; want true")
	}
}

func TestInputBufferEmptyReadWakesOnPush(t *testing.T) {
	b := NewInputBuffer(10, nil)

	wait := readEmpty(t, b).Wait()
	if isClosed(wait) {
		t.Fatal("Wait() channel closed before any Push; want open")
	}

	go func() { _ = b.Push([]byte("z")) }()

	if !waitClosed(wait) {
		t.Fatalf("Wait() channel still open %v after Push; want closed", bufferWaitTimeout)
	}
}

func TestInputBufferCloseDrainsBeforeEOF(t *testing.T) {
	rec := &readRecorder{}
	b := NewInputBuffer(10, rec.onRead)
	if err := b.Push([]byte("xy")); err != nil {
		t.Fatalf("Push(xy) = %v; want nil", err)
	}

	if err := b.Close(); err != nil {
		t.Fatalf("Close() = %v; want nil", err)
	}
	if !isClosed(b.Closed()) {
		t.Fatal("Closed() open right after Close; want closed")
	}
	if isClosed(b.Done()) {
		t.Fatal("Done() closed while data is still buffered; want open")
	}

	p := make([]byte, 8)
	n, err := b.Read(p)
	if err != nil || string(p[:n]) != "xy" {
		t.Fatalf("Read after Close = (%q, %v); want (\"xy\", nil)", p[:n], err)
	}
	if got := rec.recorded(); len(got) != 0 {
		t.Fatalf("onRead counts after Close = %v; want none", got)
	}

	n, err = b.Read(p)
	if n != 0 || err != io.EOF {
		t.Fatalf("Read on a drained closed buffer = (%d, %v); want (0, EOF)", n, err)
	}
	if !isClosed(b.Done()) {
		t.Fatal("Done() open after the buffer drained; want closed")
	}
}

func TestInputBufferPushAfterClose(t *testing.T) {
	b := NewInputBuffer(10, nil)
	if err := b.Close(); err != nil {
		t.Fatalf("Close() = %v; want nil", err)
	}

	if err := b.Push([]byte("a")); !errors.Is(err, nodes.ErrBufferClosed) {
		t.Fatalf("Push after Close = %v; want %v", err, nodes.ErrBufferClosed)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("second Close() = %v; want nil", err)
	}
}

func TestInputBufferCloseEmptyWakesReader(t *testing.T) {
	b := NewInputBuffer(10, nil)
	wait := readEmpty(t, b).Wait()

	if err := b.Close(); err != nil {
		t.Fatalf("Close() = %v; want nil", err)
	}

	if !isClosed(b.Done()) {
		t.Fatal("Done() open after closing an empty buffer; want closed")
	}
	if !isClosed(wait) {
		t.Fatal("pending Wait() channel open after Close; want closed")
	}
}
