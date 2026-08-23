package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	alog "github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/resources"
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

// TestLogFileRollsAndKeepsMaxFiles: past the size bound the pump starts a new
// file, and the logs directory keeps only the most recent ones. Pre-fix
// nothing bounded the directory at all.
func TestLogFileRollsAndKeepsMaxFiles(t *testing.T) {
	const maxFiles = 3

	// why: 200 entries past a 256-byte bound is dozens of rolls, so the
	// directory is asked to hold far more than it keeps.
	const total = 200

	root := t.TempDir()

	// why: the directory is read once the pump has finished with every entry,
	// not once the queue is empty — the queue empties one dequeue before the
	// roll that entry triggers, so the count would be read mid-roll. The buffer
	// holds every signal, so the pump never waits on this test.
	// note: total signals arrive exactly, because the queue's default 1024 caps
	// nothing here and no entry is dropped.
	pumped := make(chan struct{}, total)
	restore := logFilePumped
	logFilePumped = func() { pumped <- struct{}{} }
	t.Cleanup(func() { logFilePumped = restore })

	lf, err := CreateLogFile(testIdentity(), root, 256, maxFiles)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < total; i++ {
		lf.LogEntry(alog.NewEntry(lf.origin, 0, astral.NewString32(fmt.Sprintf("e%d", i))))
	}

	deadline := time.After(5 * time.Second)
	for i := 0; i < total; i++ {
		select {
		case <-pumped:
		case <-deadline:
			t.Fatalf("the pump handled %d of %d entries", i, total)
		}
	}

	dir := filepath.Join(root, logDirName)

	names := logFileNames(t, dir)
	if len(names) != maxFiles {
		t.Fatalf("%v holds %d files, want %d: %v", dir, len(names), maxFiles, names)
	}

	// why: a file that never rolled would hold everything, and the count above
	// would pass on a directory the node simply never filled.
	if size := fileSize(t, filepath.Join(dir, names[len(names)-1])); size == 0 {
		t.Fatal("the newest file is empty: the roll left the sink behind")
	}
}

// TestLogFilePruneKeepsTheActiveFile: prune deletes by name, so names it did
// not write — a stamp from a clock that was ahead, a restored backup — make the
// file the pump just opened the oldest one in the directory. Pre-fix prune
// trusted the active file to sort last and deleted it, leaving the pump
// appending to an unlinked descriptor.
func TestLogFilePruneKeepsTheActiveFile(t *testing.T) {
	const maxFiles = 2

	root := t.TempDir()
	dir := filepath.Join(root, logDirName)

	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatal(err)
	}

	// why: the year leads the stamp, so these sort after every name this process
	// can produce, and maxFiles of them put the active file first in the window
	// prune deletes.
	for _, name := range []string{"2099-01-01_00-00-00", "2099-01-01_00-00-01"} {
		if err := os.WriteFile(filepath.Join(dir, logFilePrefix+name), []byte("skewed\n"), 0640); err != nil {
			t.Fatal(err)
		}
	}

	lf, err := CreateLogFile(testIdentity(), root, 0, maxFiles)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(lf.path); err != nil {
		t.Fatalf("prune deleted the file the pump holds open: %v", err)
	}

	if names := logFileNames(t, dir); len(names) != maxFiles {
		t.Fatalf("%v holds %d files, want %d: %v", dir, len(names), maxFiles, names)
	}
}

// TestLogFileDisabledCreatesNothing: with file off the module touches no disk,
// so the logs directory does not come into existence.
func TestLogFileDisabledCreatesNothing(t *testing.T) {
	root := t.TempDir()

	res, err := resources.NewFileResources(filepath.Join(root, "config"), true)
	if err != nil {
		t.Fatal(err)
	}
	res.SetDataRoot(filepath.Join(root, "data"))

	var config Config
	config.setDefaults()
	config.File = false

	addLogFile(alog.New(testIdentity()), testIdentity(), res, &config)

	dir := filepath.Join(root, "data", logDirName)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("%v exists with file off: %v", dir, err)
	}

	// why: the same call with the file on must reach the same directory, or
	// the assertion above holds for the wrong reason.
	config.File = true
	addLogFile(alog.New(testIdentity()), testIdentity(), res, &config)

	if names := logFileNames(t, dir); len(names) != 1 {
		t.Fatalf("%v holds %d files with file on, want 1: %v", dir, len(names), names)
	}
}

func logFileNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	return info.Size()
}
