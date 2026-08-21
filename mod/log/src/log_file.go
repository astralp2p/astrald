package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// LogFile appends every entry the root logger emits to a file. LogEntry runs
// under the root logger mutex, so it only enqueues; a single pump goroutine is
// the file's sole writer, which also keeps entries in production order.
type LogFile struct {
	ch        *channel.Channel
	path      string
	origin    *astral.Identity
	queue     chan *log.Entry
	dropped   atomic.Uint64
	firstDrop atomic.Int64 // unix nanoseconds of the oldest unreported drop
}

var _ log.EntryLogger = &LogFile{}

func CreateLogFile(origin *astral.Identity) (*LogFile, error) {
	path := "astrald.log." + time.Now().Format("2006-01-02_15-04-05")
	if home, err := os.UserHomeDir(); err == nil {
		dirPath := filepath.Join(home, ".config", "astrald", "logs")
		os.MkdirAll(dirPath, 0750)
		path = filepath.Join(dirPath, path)
	}

	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	return newLogFile(file, path, origin), nil
}

// newLogFile starts the pump with the file. why: a LogFile without its pump
// accepts entries and writes none.
func newLogFile(w io.Writer, path string, origin *astral.Identity) *LogFile {
	f := &LogFile{
		path:   path,
		origin: origin,
		queue:  make(chan *log.Entry, logFileQueueCap),
		ch: channel.New(streams.ReadWriteCloseSplit{
			Reader: nil,
			Writer: w,
			Closer: nil,
		}),
	}

	go f.pump()

	return f
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
	}
}

func (l *LogFile) write(entry *log.Entry) {
	if err := l.ch.Send(entry); err != nil {
		fmt.Println("log write error:", err)
	}
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
