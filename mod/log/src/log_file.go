package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/streams"
)

// note: a local append costs microseconds, so the queue only absorbs bursts and
// transient disk stalls; var so tests can shorten it.
var logFileQueueCap = 1024

// logDirName is the directory under the node's data root that holds log files.
const logDirName = "logs"

// logFilePrefix names a log file and, by the same token, identifies one: the
// logs directory is pruned by prefix, so nothing else in it is touched.
const logFilePrefix = "astrald.log."

// logFileStamp resolves to the second, and nextPath appends an ordinal when
// that is not enough. why: the stamp orders the files this process wrote
// lexicographically, which is the order prune deletes them in.
const logFileStamp = "2006-01-02_15-04-05"

// logFileMaxOrdinal bounds the ordinal nextPath appends within one second.
const logFileMaxOrdinal = 1000

// logFilePumped, when set, fires on the pump goroutine once an entry is fully
// handled: written, and any roll it triggered carried through prune. why: the
// queue empties when the pump takes an entry, not when it is done with it, so
// a test that watched the queue alone could read the logs directory between a
// roll's OpenFile and its prune, where it holds one file more than it keeps.
// note: a constructor copies it into the LogFile before starting the pump, and
// the field is never written again; a test therefore sets this before it builds
// the LogFile, and pumps left running by earlier tests keep what they captured.
var logFilePumped func()

// LogFile appends every entry the root logger emits to a file. LogEntry runs
// under the root logger mutex, so it only enqueues; a single pump goroutine is
// the file's sole writer, which also keeps entries in production order.
type LogFile struct {
	ch        *channel.Channel
	file      *os.File // nil when the sink is not a file, which is to say in tests
	dir       string
	path      string
	stamp     string // the second the current file's name was taken from
	ordinal   int    // the next ordinal to append to that stamp
	written   int64  // bytes in the current file; the pump alone reads and writes it
	maxSize   int64
	maxFiles  int
	origin    *astral.Identity
	pumped    func() // test hook; see logFilePumped
	queue     chan *log.Entry
	dropped   atomic.Uint64
	firstDrop atomic.Int64 // unix nanoseconds of the oldest unreported drop
}

var _ log.EntryLogger = &LogFile{}

// CreateLogFile opens the node's log file in dataRoot, the directory the node
// resolved for its data — <root>/data under -root. why: the log is node state,
// so a container that replaces its image keeps its logs and a node that is
// deleted takes them with it.
func CreateLogFile(origin *astral.Identity, dataRoot string, maxSize int64, maxFiles int) (*LogFile, error) {
	var l = &LogFile{
		dir:      filepath.Join(dataRoot, logDirName),
		maxSize:  maxSize,
		maxFiles: maxFiles,
		origin:   origin,
		pumped:   logFilePumped,
		queue:    make(chan *log.Entry, logFileQueueCap),
	}

	if err := os.MkdirAll(l.dir, 0750); err != nil {
		return nil, err
	}

	if err := l.open(); err != nil {
		return nil, err
	}

	go l.pump()

	return l, nil
}

// newLogFile starts the pump with the writer. why: a LogFile without its pump
// accepts entries and writes none.
// note: a LogFile built this way holds no file and never rolls; the size bound
// belongs to the logs directory, which only CreateLogFile owns.
func newLogFile(w io.Writer, path string, origin *astral.Identity) *LogFile {
	f := &LogFile{
		path:   path,
		origin: origin,
		pumped: logFilePumped,
		queue:  make(chan *log.Entry, logFileQueueCap),
	}

	f.setSink(w)

	go f.pump()

	return f
}

// open starts a new file in the logs directory and makes it the sink, then
// prunes what the directory no longer keeps.
func (l *LogFile) open() error {
	var path = l.nextPath()

	// note: the file is opened for append rather than created, so a stamp that
	// an earlier run already used continues that file instead of erasing it.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0640)
	if err != nil {
		return err
	}

	l.file, l.path, l.written = file, path, 0
	if info, err := file.Stat(); err == nil {
		l.written = info.Size()
	}

	l.setSink(file)
	l.prune()

	return nil
}

// nextPath is the stamped path of the next file, extended with an ordinal when
// the stamp is taken. why: a roll can follow the previous one within the same
// second, and two rolls into one name are one file, not two.
// note: the ordinal only ever climbs within a second, so a name prune has
// deleted is never handed out again; were it reused, the new file would sort
// among the oldest and prune would delete the files that actually precede it.
// The new file itself survives either way — prune excludes the active one.
func (l *LogFile) nextPath() string {
	var stamp = time.Now().Format(logFileStamp)
	if stamp != l.stamp {
		l.stamp, l.ordinal = stamp, 0
	}

	var base = filepath.Join(l.dir, logFilePrefix+stamp)

	for l.ordinal < logFileMaxOrdinal {
		// note: the ordinal is zero-padded so that it sorts with the stamp.
		var path = base
		if l.ordinal > 0 {
			path = fmt.Sprintf("%s.%03d", base, l.ordinal)
		}
		l.ordinal++

		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
	}

	// note: a second that exhausts its ordinals appends to its last file until
	// the clock moves on; nothing is lost, the roll only waits.
	return fmt.Sprintf("%s.%03d", base, logFileMaxOrdinal-1)
}

// prune deletes the oldest files beyond maxFiles, never the file the pump is
// writing. why: the size bound alone bounds one file, not the directory.
// note: prune runs only from open, which runs on the pump goroutine — the sole
// writer of l.path — so it reads the active path without a lock.
func (l *LogFile) prune() {
	if l.maxFiles <= 0 {
		return
	}

	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return
	}

	// note: os.ReadDir sorts by name, and nextPath hands out names in ascending
	// order, so name order is open order — for the names this process wrote.
	// A name it did not write need not obey that order: a stamp from a clock that
	// was ahead, a restored backup, a file another run left behind all sort after
	// the active file and push it into the window this loop deletes. The active
	// file is therefore excluded by identity rather than trusted to sort last;
	// deleting it leaves the pump appending to an unlinked descriptor.
	var active = filepath.Base(l.path)

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), logFilePrefix) {
			continue
		}
		if entry.Name() == active {
			continue
		}
		names = append(names, entry.Name())
	}

	// note: the active file is one of the maxFiles the directory keeps, so
	// maxFiles-1 of the rest survive.
	for i := 0; i < len(names)-(l.maxFiles-1); i++ {
		os.Remove(filepath.Join(l.dir, names[i]))
	}
}

// setSink points the channel at w and counts what reaches it.
func (l *LogFile) setSink(w io.Writer) {
	l.ch = channel.New(streams.ReadWriteCloseSplit{
		Reader: nil,
		Writer: &countingWriter{w: w, n: &l.written},
		Closer: nil,
	})
}

// LogEntry enqueues the entry without blocking and counts a drop when the queue
// is full. why: the root logger holds its mutex across the whole fan-out, so a
// write that parks here parks every goroutine in the node that logs.
func (l *LogFile) LogEntry(entry *log.Entry) {
	select {
	case l.queue <- entry:
	default:
		if l.dropped.Add(1) == 1 {
			l.firstDrop.Store(time.Now().UnixNano())
		}
	}
}

// pump drains the queue for the process lifetime. why: the log file has no
// session to end, so a stalled disk degrades to dropped entries rather than
// closing the only on-disk record.
// note: entries still queued when the process dies are lost. The queue is empty
// within microseconds unless the disk is already stalled, and core/run.go waits
// out its flush grace after the modules stop.
func (l *LogFile) pump() {
	for entry := range l.queue {
		l.reportDrops()
		l.write(entry)
		l.roll()

		if l.pumped != nil {
			l.pumped()
		}
	}
}

func (l *LogFile) write(entry *log.Entry) {
	if err := l.ch.Send(entry); err != nil {
		fmt.Println("log write error:", err)
	}
}

// roll starts a new file once the current one passes maxSize. why: the pump is
// the file's sole writer, so it is the only place the sink can be swapped
// without a lock.
// note: the old file is closed only after the new one opens, so a directory
// that stops accepting files leaves the node writing to the file it has.
func (l *LogFile) roll() {
	if l.file == nil || l.maxSize <= 0 || l.written < l.maxSize {
		return
	}

	var old = l.file

	if err := l.open(); err != nil {
		fmt.Println("log roll error:", err)
		// note: the tally restarts, so a directory that stops accepting files
		// is retried once per maxSize bytes rather than once per entry.
		l.written = 0
		return
	}

	old.Close()
}

// reportDrops writes accumulated overflow as an in-band entry stamped with the
// first drop's time. why: a gap in the log file is otherwise indistinguishable
// from a node that had nothing to say.
func (l *LogFile) reportDrops() {
	n := l.dropped.Swap(0)
	if n == 0 {
		return
	}

	l.write(&log.Entry{
		Origin: l.origin,
		Time:   astral.Time(time.Unix(0, l.firstDrop.Load())),
		Objects: []astral.Object{
			astral.NewString32(fmt.Sprintf("log file: dropped %d entries", n)),
		},
	})
}

// countingWriter tallies what reaches the file. why: the roll threshold is the
// file's size, and the pump would otherwise stat the file for every entry.
// note: the counter is the pump's alone — the pump is the sole writer — so it
// needs no synchronisation.
type countingWriter struct {
	w io.Writer
	n *int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	*c.n += int64(n)
	return n, err
}
