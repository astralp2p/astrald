package nodes

import (
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astrald/mod/nodes"
)

type writeResult struct {
	n   int
	err error
}

// note: the writer is closed on cleanup, which releases a Write left blocked by a failed test.
func newTestSessionWriter(t *testing.T, reset func()) (*muxSessionWriter, *writeRecorder) {
	t.Helper()
	rec := &writeRecorder{}
	w := newSessionWriter(NewOutputBuffer(rec.write), reset)
	t.Cleanup(func() { _ = w.PeerClose() })
	return w, rec
}

func startWrite(w *muxSessionWriter, p []byte) <-chan writeResult {
	ch := make(chan writeResult, 1)
	go func() {
		n, err := w.Write(p)
		ch <- writeResult{n, err}
	}()
	return ch
}

func requireWriteBlocked(t *testing.T, ch <-chan writeResult) {
	t.Helper()
	select {
	case res := <-ch:
		t.Fatalf("Write returned (%d, %v); want it to block", res.n, res.err)
	case <-time.After(blockWindow):
	}
}

func awaitWrite(t *testing.T, ch <-chan writeResult) writeResult {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(bufferWaitTimeout):
		t.Fatalf("Write still blocked after %v; want it to return", bufferWaitTimeout)
		return writeResult{}
	}
}

func TestSessionWriterWaitsForCredit(t *testing.T) {
	w, rec := newTestSessionWriter(t, nil)

	res := startWrite(w, []byte("hello"))
	requireWriteBlocked(t, res)

	go func() {
		w.Grow(2)
		w.Grow(3)
	}()

	if got := awaitWrite(t, res); got.n != 5 || got.err != nil {
		t.Fatalf("Write = (%d, %v); want (5, nil)", got.n, got.err)
	}
	if got := rec.recorded(); got != "hello" {
		t.Fatalf("write callback got %q; want %q", got, "hello")
	}
}

func TestSessionWriterCloseResetsOnce(t *testing.T) {
	var resets atomic.Uint64
	w, _ := newTestSessionWriter(t, func() { resets.Add(1) })
	w.Grow(8)

	for i := 0; i < 2; i++ {
		if err := w.Close(); err != nil {
			t.Fatalf("Close() #%d = %v; want nil", i+1, err)
		}
	}
	if got := resets.Load(); got != 1 {
		t.Fatalf("reset called %d times; want 1", got)
	}

	n, err := w.Write([]byte("abc"))
	if n != 0 || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Write after Close = (%d, %v); want (0, %v)", n, err, io.ErrClosedPipe)
	}
}

func TestSessionWriterPeerCloseSkipsReset(t *testing.T) {
	var resets atomic.Uint64
	w, _ := newTestSessionWriter(t, func() { resets.Add(1) })
	w.Grow(8)

	if err := w.PeerClose(); err != nil {
		t.Fatalf("PeerClose() = %v; want nil", err)
	}
	if got := resets.Load(); got != 0 {
		t.Fatalf("reset called %d times after PeerClose; want 0", got)
	}

	n, err := w.Write([]byte("abc"))
	if n != 0 || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Write after PeerClose = (%d, %v); want (0, %v)", n, err, io.ErrClosedPipe)
	}
}

func TestSessionWriterPauseBlocksUntilResume(t *testing.T) {
	w, rec := newTestSessionWriter(t, nil)
	w.Grow(8)
	w.Pause()

	res := startWrite(w, []byte("abc"))
	requireWriteBlocked(t, res)
	if got := rec.recorded(); got != "" {
		t.Fatalf("write callback got %q while paused; want nothing", got)
	}

	w.Resume()

	if got := awaitWrite(t, res); got.n != 3 || got.err != nil {
		t.Fatalf("Write after Resume = (%d, %v); want (3, nil)", got.n, got.err)
	}
	if got := rec.recorded(); got != "abc" {
		t.Fatalf("write callback got %q; want %q", got, "abc")
	}
}

func TestSessionWriterSwapBuf(t *testing.T) {
	var oldResets, newResets atomic.Uint64
	w, _ := newTestSessionWriter(t, func() { oldResets.Add(1) })
	oldBuf := w.Buf()
	oldBuf.Grow(8)

	newBuf := NewOutputBuffer((&writeRecorder{}).write)
	w.SwapBuf(newBuf, func() { newResets.Add(1) })

	if w.Buf() != newBuf {
		t.Fatal("Buf() after SwapBuf is not the new buffer")
	}
	if n, err := oldBuf.Write([]byte("abc")); n != 0 || !errors.Is(err, nodes.ErrBufferClosed) {
		t.Fatalf("old buffer Write = (%d, %v); want (0, %v)", n, err, nodes.ErrBufferClosed)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v; want nil", err)
	}
	if got := newResets.Load(); got != 1 {
		t.Fatalf("new reset called %d times; want 1", got)
	}
	if got := oldResets.Load(); got != 0 {
		t.Fatalf("original reset called %d times; want 0", got)
	}
}
