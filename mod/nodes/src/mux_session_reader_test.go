package nodes

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astrald/mod/nodes"
)

type readResult struct {
	data string
	err  error
}

// note: the reader is closed on cleanup, which releases a Read left blocked by a failed test.
func newTestSessionReader(t *testing.T, buf *InputBuffer) *muxSessionReader {
	t.Helper()
	r := newSessionReader(buf)
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func startRead(r *muxSessionReader) <-chan readResult {
	ch := make(chan readResult, 1)
	go func() {
		p := make([]byte, 16)
		n, err := r.Read(p)
		ch <- readResult{string(p[:n]), err}
	}()
	return ch
}

func requireReadBlocked(t *testing.T, ch <-chan readResult) {
	t.Helper()
	select {
	case res := <-ch:
		t.Fatalf("Read returned (%q, %v); want it to block", res.data, res.err)
	case <-time.After(blockWindow):
	}
}

func awaitRead(t *testing.T, ch <-chan readResult) readResult {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(bufferWaitTimeout):
		t.Fatalf("Read still blocked after %v; want it to return", bufferWaitTimeout)
		return readResult{}
	}
}

func readString(t *testing.T, r *muxSessionReader) string {
	t.Helper()
	res := awaitRead(t, startRead(r))
	if res.err != nil {
		t.Fatalf("Read = (%q, %v); want nil error", res.data, res.err)
	}
	return res.data
}

func TestSessionReaderAdvanceKeepsOldDataFirst(t *testing.T) {
	a, b := NewInputBuffer(16, nil), NewInputBuffer(16, nil)
	r := newTestSessionReader(t, a)

	if err := a.Push([]byte("old")); err != nil {
		t.Fatalf("a.Push(old) = %v; want nil", err)
	}
	r.SetNextBuffer(b)
	r.Advance()

	if r.Buf() != a {
		t.Fatal("Buf() after Advance with undrained data is not the old buffer")
	}
	if !isClosed(a.Closed()) {
		t.Fatal("old buffer open after Advance; want closed")
	}

	if err := r.Push([]byte("new")); err != nil {
		t.Fatalf("Push after Advance = %v; want nil (routed to the next buffer)", err)
	}

	for _, want := range []string{"old", "new"} {
		if got := readString(t, r); got != want {
			t.Fatalf("Read = %q; want %q", got, want)
		}
	}
	if r.Buf() != b {
		t.Fatal("Buf() after draining the old buffer is not the next buffer")
	}
}

func TestSessionReaderAdvanceEmptySwitchesAtOnce(t *testing.T) {
	a, b := NewInputBuffer(16, nil), NewInputBuffer(16, nil)
	r := newTestSessionReader(t, a)

	r.SetNextBuffer(b)
	r.Advance()

	if r.Buf() != b {
		t.Fatal("Buf() after Advance on an empty buffer is not the next buffer")
	}
}

func TestSessionReaderAdvanceWithoutNextBuffer(t *testing.T) {
	a := NewInputBuffer(16, nil)
	r := newTestSessionReader(t, a)

	r.Advance()

	if r.Buf() != a {
		t.Fatal("Buf() changed after Advance with no next buffer")
	}
	if isClosed(a.Closed()) {
		t.Fatal("buffer closed by Advance with no next buffer; want open")
	}
	if err := r.Push([]byte("abc")); err != nil {
		t.Fatalf("Push = %v; want nil", err)
	}
	if a.IsEmpty() {
		t.Fatal("current buffer empty after Push; want the pushed bytes")
	}
}

func TestSessionReaderReadWaitsForPush(t *testing.T) {
	r := newTestSessionReader(t, NewInputBuffer(16, nil))

	res := startRead(r)
	requireReadBlocked(t, res)

	if err := r.Push([]byte("late")); err != nil {
		t.Fatalf("Push = %v; want nil", err)
	}

	if got := awaitRead(t, res); got.data != "late" || got.err != nil {
		t.Fatalf("Read = (%q, %v); want (\"late\", nil)", got.data, got.err)
	}
}

func TestSessionReaderPauseBlocksBufferedData(t *testing.T) {
	r := newTestSessionReader(t, NewInputBuffer(16, nil))
	if err := r.Push([]byte("abc")); err != nil {
		t.Fatalf("Push = %v; want nil", err)
	}
	r.Pause()

	res := startRead(r)
	requireReadBlocked(t, res)

	r.Resume()

	if got := awaitRead(t, res); got.data != "abc" || got.err != nil {
		t.Fatalf("Read after Resume = (%q, %v); want (\"abc\", nil)", got.data, got.err)
	}
}

func TestSessionReaderCloseReleasesPausedRead(t *testing.T) {
	r := newTestSessionReader(t, NewInputBuffer(16, nil))
	r.Pause()

	res := startRead(r)
	requireReadBlocked(t, res)

	if err := r.Close(); err != nil {
		t.Fatalf("Close() = %v; want nil", err)
	}

	if got := awaitRead(t, res); got.data != "" || got.err != io.EOF {
		t.Fatalf("Read after Close = (%q, %v); want (\"\", EOF)", got.data, got.err)
	}
	if err := r.Push([]byte("abc")); !errors.Is(err, nodes.ErrBufferClosed) {
		t.Fatalf("Push after Close = %v; want %v", err, nodes.ErrBufferClosed)
	}
}
