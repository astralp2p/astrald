package log

import (
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	alog "github.com/astralp2p/astral-go/astral/log"
)

// gatedWriter emulates a disk that stopped accepting writes: every Write parks
// until release, then forwards to w.
type gatedWriter struct {
	gate     chan struct{}
	released atomic.Bool
	w        io.Writer
}

func newGatedWriter(w io.Writer) *gatedWriter {
	return &gatedWriter{gate: make(chan struct{}), w: w}
}

func (g *gatedWriter) Write(p []byte) (int, error) {
	<-g.gate
	return g.w.Write(p)
}

func (g *gatedWriter) release() {
	if g.released.CompareAndSwap(false, true) {
		close(g.gate)
	}
}

// countingSink stands in for every other EntryLogger on the same root: it never
// blocks, so anything it misses was withheld by the fan-out, not by itself.
type countingSink struct {
	got chan *alog.Entry
}

func (s *countingSink) LogEntry(e *alog.Entry) {
	select {
	case s.got <- e:
	default:
	}
}

// TestLogFileStalledWriteDoesNotBlockLogging is the regression test: with the
// log file's writer parked, every Log call must still return and the fan-out
// must still reach the subscribers behind the log file. Pre-fix the synchronous
// ch.Send ran inside the root logger's fan-out, which holds the root mutex, so
// this test deadlocks.
func TestLogFileStalledWriteDoesNotBlockLogging(t *testing.T) {
	restore := logFileQueueCap
	logFileQueueCap = 8
	t.Cleanup(func() { logFileQueueCap = restore })

	const total = 4 * 8

	id := testIdentity()
	logger := alog.New(id)
	logger.SetFilter(func(*alog.Entry) bool { return false }) // note: gates stdout only, not subscribers

	stalled := newGatedWriter(io.Discard)
	t.Cleanup(stalled.release)

	lf := newLogFile(stalled, "test", id)

	logger.AddLogger(lf)

	sink := &countingSink{got: make(chan *alog.Entry, total)}
	logger.AddLogger(sink) // note: registered behind lf, so lf's fan-out slot gates it

	// why: an unrelated child logger is what every module holds; it shares the
	// root's mutex, which is the lock a synchronous file write holds.
	other := logger.Tag(alog.Tag("mod/other"))

	logged := make(chan struct{})
	go func() {
		defer close(logged)
		for i := 0; i < total; i++ {
			other.Log("entry %v", i)
		}
	}()

	select {
	case <-logged:
	case <-time.After(3 * time.Second):
		t.Fatal("logging blocked behind a stalled log file write")
	}

	if len(sink.got) != total {
		t.Fatalf("sink got %d of %d entries: the fan-out stalled", len(sink.got), total)
	}
}

// TestLogFileOrderAndDropConservation: with the writer parked the queue
// overflows; once writes go through, the file holds the surviving entries in
// production order and the in-band notice accounts for every entry that never
// landed.
func TestLogFileOrderAndDropConservation(t *testing.T) {
	restore := logFileQueueCap
	logFileQueueCap = 4
	t.Cleanup(func() { logFileQueueCap = restore })

	pr, pw := io.Pipe()
	t.Cleanup(func() { pr.Close() })

	stalled := newGatedWriter(pw)

	lf := newLogFile(stalled, "test", testIdentity())

	const total = 12
	for i := 0; i < total; i++ {
		lf.LogEntry(alog.NewEntry(lf.origin, 0, astral.NewString32(fmt.Sprintf("e%d", i))))
	}

	stalled.release() // every entry the queue still holds now reaches the file

	var written, reportedDropped, lastIdx = 0, 0, -1
	entries := receiveEntries(channel.NewReceiver(pr))
	deadline := time.After(5 * time.Second)

	for written+reportedDropped < total {
		select {
		case e := <-entries:
			text := entryText(e)
			if n, ok := strings.CutPrefix(text, "log file: dropped "); ok {
				var count int
				if _, err := fmt.Sscanf(n, "%d entries", &count); err != nil {
					t.Fatalf("malformed drop notice %q: %v", text, err)
				}
				reportedDropped += count
				continue
			}
			var idx int
			if _, err := fmt.Sscanf(text, "e%d", &idx); err != nil {
				t.Fatalf("unexpected entry %q: %v", text, err)
			}
			if idx <= lastIdx {
				t.Fatalf("entry e%d written after e%d: order lost", idx, lastIdx)
			}
			lastIdx = idx
			written++
		case <-deadline:
			t.Fatalf("file went quiet: wrote %d + dropped %d of %d", written, reportedDropped, total)
		}
	}

	if reportedDropped == 0 {
		t.Fatal("queue of 4 absorbed 12 entries without drops: overflow untested")
	}
}
