package log

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
)

// note: defaults per protocols/log/ops/log.listen.md; vars so tests can shorten them.
var (
	listenQueueCap    = 256
	listenSendTimeout = 5 * time.Second
)

type opListenArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

func (mod *Module) OpListen(ctx *astral.Context, q *routing.IncomingQuery, args opListenArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	fwd := newLogForwarder(ch, mod.node.Identity())
	go fwd.pump()
	defer close(fwd.done)

	mod.log.AddLogger(fwd)
	defer mod.log.RemoveLogger(fwd)

	for {
		_, err := ch.Receive()
		if err != nil {
			return nil
		}
	}
}

// logForwarder forwards log entries to one log.listen subscriber. LogEntry runs
// under the root logger mutex, so it only enqueues; a single pump goroutine is
// the channel's sole writer, which also keeps frames from interleaving.
type logForwarder struct {
	ch          *channel.Channel
	origin      *astral.Identity
	queue       chan *log.Entry
	done        chan struct{}
	sendTimeout time.Duration // snapshot of listenSendTimeout at session start
	dropped     atomic.Uint64
	firstDrop   atomic.Int64 // unix nanoseconds of the oldest unreported drop
}

func newLogForwarder(ch *channel.Channel, origin *astral.Identity) *logForwarder {
	return &logForwarder{
		ch:          ch,
		origin:      origin,
		queue:       make(chan *log.Entry, listenQueueCap),
		done:        make(chan struct{}),
		sendTimeout: listenSendTimeout,
	}
}

// LogEntry enqueues the entry without blocking and counts a drop when the queue
// is full. why: a blocking send here parks inside the root logger mutex and
// freezes every logging goroutine in the node.
func (fwd *logForwarder) LogEntry(entry *log.Entry) {
	select {
	case fwd.queue <- entry:
	default:
		if fwd.dropped.Add(1) == 1 {
			fwd.firstDrop.Store(time.Now().UnixNano())
		}
	}
}

// pump drains the queue as the channel's sole writer; it ends on send error or
// when OpListen returns. Entries left behind are discarded with the session.
// note: the queue is never closed — a fan-out that raced RemoveLogger may still
// enqueue after the pump is gone.
func (fwd *logForwarder) pump() {
	for {
		select {
		case entry := <-fwd.queue:
			if fwd.sendDropNotice() != nil || fwd.send(entry) != nil {
				return
			}
		case <-fwd.done:
			return
		}
	}
}

// send writes one entry under a watchdog. why: the transport has no write
// deadline; closing the channel is the only way to unblock a stalled Send, and
// the close also ends OpListen's Receive loop so the deferred RemoveLogger runs.
func (fwd *logForwarder) send(entry *log.Entry) error {
	watchdog := time.AfterFunc(fwd.sendTimeout, func() { fwd.ch.Close() })
	defer watchdog.Stop()
	return fwd.ch.Send(entry)
}

// sendDropNotice reports accumulated drops as an in-band entry stamped with the
// first drop's time, per protocols/log/ops/log.listen.md.
func (fwd *logForwarder) sendDropNotice() error {
	n := fwd.dropped.Swap(0)
	if n == 0 {
		return nil
	}

	return fwd.send(&log.Entry{
		Origin: fwd.origin,
		Time:   astral.Time(time.Unix(0, fwd.firstDrop.Load())),
		Objects: []astral.Object{
			astral.NewString32(fmt.Sprintf("log.listen: dropped %d entries", n)),
		},
	})
}
