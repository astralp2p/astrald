package log

import (
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	alog "github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/streams"
)

// blockingWriteCloser emulates a subscriber that stopped reading: every Write
// parks until Close, then fails with io.ErrClosedPipe.
type blockingWriteCloser struct {
	closed   chan struct{}
	isClosed atomic.Bool
}

func newBlockingWriteCloser() *blockingWriteCloser {
	return &blockingWriteCloser{closed: make(chan struct{})}
}

func (b *blockingWriteCloser) Write(p []byte) (int, error) {
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *blockingWriteCloser) Close() error {
	if b.isClosed.CompareAndSwap(false, true) {
		close(b.closed)
	}
	return nil
}

// stubNode satisfies astral.Node for the drop-notice origin.
type stubNode struct {
	id *astral.Identity
}

func (n stubNode) Identity() *astral.Identity { return n.id }

func (n stubNode) RouteQuery(*astral.Context, *astral.InFlightQuery, io.WriteCloser) (io.WriteCloser, error) {
	return nil, astral.NewErrRejected(0)
}

func testIdentity() *astral.Identity {
	return secp256k1.Identity(secp256k1.PublicKey(secp256k1.New()))
}

type listenRig struct {
	mod    *Module
	opDone chan struct{}
}

// startListen runs OpListen against remote as the subscriber's write side and
// returns once the query is accepted. For a blockingWriteCloser it emulates
// core's direction coupling: closing the write side also ends the op's reader,
// which core.conn.Close does in production (core/conn.go).
func startListen(t *testing.T, remote io.WriteCloser) *listenRig {
	t.Helper()

	id := testIdentity()
	logger := alog.New(id)
	logger.SetFilter(func(*alog.Entry) bool { return false }) // note: gates stdout only, not subscribers

	mod := &Module{log: logger, node: stubNode{id: id}}

	op, err := routing.NewOp(mod.OpListen)
	if err != nil {
		t.Fatal(err)
	}

	opDone := make(chan struct{})
	op.LogFunc = func(*routing.Report) { close(opDone) }

	q := astral.Launch(&astral.Query{QueryString: "log.listen", Caller: id, Target: id})

	localWriter, err := op.RouteQuery(astral.NewContext(nil), q, remote)
	if err != nil {
		t.Fatal(err)
	}

	if b, ok := remote.(*blockingWriteCloser); ok {
		go func() {
			<-b.closed
			localWriter.Close()
		}()
	}

	t.Cleanup(func() {
		localWriter.Close()
		select {
		case <-opDone:
		case <-time.After(5 * time.Second):
			t.Error("OpListen did not return on teardown")
		}
	})

	return &listenRig{mod: mod, opDone: opDone}
}

// TestListenStalledSubscriberDoesNotBlockLogging is the regression test for the
// whole-node freeze: with a subscriber that never reads, every Log call must
// still return. Pre-fix the forwarder's blocking Send parked inside the root
// logger mutex and this test deadlocks.
func TestListenStalledSubscriberDoesNotBlockLogging(t *testing.T) {
	restore := listenSendTimeout
	listenSendTimeout = time.Hour // why: keep the watchdog disconnect out of this test
	t.Cleanup(func() { listenSendTimeout = restore })

	rig := startListen(t, newBlockingWriteCloser())

	logged := make(chan struct{})
	go func() {
		for i := 0; i < 3*listenQueueCap; i++ {
			rig.mod.log.Log("entry %v", i)
		}
		close(logged)
	}()

	select {
	case <-logged:
	case <-time.After(3 * time.Second):
		t.Fatal("logging blocked behind a stalled log.listen subscriber")
	}
}

// TestListenWatchdogDisconnectsStalledSubscriber: a send stalled past
// listenSendTimeout closes the channel, which must end OpListen (and with it
// the deferred RemoveLogger) while the peer's write side stays parked.
func TestListenWatchdogDisconnectsStalledSubscriber(t *testing.T) {
	restore := listenSendTimeout
	listenSendTimeout = 50 * time.Millisecond
	t.Cleanup(func() { listenSendTimeout = restore })

	rig := startListen(t, newBlockingWriteCloser())

	// note: logs repeat because registration happens inside the op goroutine;
	// once registered, one entry parks the pump and arms the watchdog.
	deadline := time.After(3 * time.Second)
	for {
		rig.mod.log.Log("wedge")
		select {
		case <-rig.opDone:
			return
		case <-deadline:
			t.Fatal("watchdog did not disconnect the stalled subscriber")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// forwarderRig drives a logForwarder directly over a real pipe-backed channel,
// bypassing OpListen: LogEntry is called from one goroutine, as the root
// logger's fan-out does.
func startForwarder(t *testing.T) (*logForwarder, channel.Receiver) {
	t.Helper()

	pr, pw := io.Pipe()
	ch := channel.New(streams.ReadWriteCloseSplit{Writer: pw, Closer: pw})

	fwd := newLogForwarder(ch, testIdentity())
	go fwd.pump()

	t.Cleanup(func() {
		close(fwd.done)
		ch.Close()
		pr.Close()
	})

	return fwd, channel.NewReceiver(pr)
}

// receiveEntries decodes entries off the receiver into a channel until an error.
func receiveEntries(rcv channel.Receiver) <-chan *alog.Entry {
	out := make(chan *alog.Entry)
	go func() {
		defer close(out)
		for {
			obj, err := rcv.Receive()
			if err != nil {
				return
			}
			if e, ok := obj.(*alog.Entry); ok {
				out <- e
			}
		}
	}()
	return out
}

func entryText(e *alog.Entry) string {
	var sb strings.Builder
	for _, o := range e.Objects {
		if s, ok := o.(*astral.String32); ok {
			sb.WriteString(string(*s))
		}
	}
	return sb.String()
}

// TestForwarderOrderAndDropConservation: with the pump parked on an unread
// pipe, the queue overflows and drops; once the reader drains, delivered
// entries arrive in production order, correctly framed, and the in-band drop
// notice accounts for every entry that was not delivered.
func TestForwarderOrderAndDropConservation(t *testing.T) {
	restoreCap, restoreTO := listenQueueCap, listenSendTimeout
	listenQueueCap, listenSendTimeout = 4, time.Hour
	t.Cleanup(func() { listenQueueCap, listenSendTimeout = restoreCap, restoreTO })

	fwd, rcv := startForwarder(t)

	const total = 12
	for i := 0; i < total; i++ {
		fwd.LogEntry(alog.NewEntry(fwd.origin, 0, astral.NewString32(fmt.Sprintf("e%d", i))))
	}

	var delivered, reportedDropped, lastIdx = 0, 0, -1
	entries := receiveEntries(rcv)
	deadline := time.After(5 * time.Second)

	for delivered+reportedDropped < total {
		select {
		case e := <-entries:
			text := entryText(e)
			if n, ok := strings.CutPrefix(text, "log.listen: dropped "); ok {
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
				t.Fatalf("entry e%d delivered after e%d: order lost", idx, lastIdx)
			}
			lastIdx = idx
			delivered++
		case <-deadline:
			t.Fatalf("stream dried up: delivered %d + dropped %d of %d", delivered, reportedDropped, total)
		}
	}

	if delivered+reportedDropped != total {
		t.Fatalf("conservation broken: delivered %d + dropped %d != sent %d", delivered, reportedDropped, total)
	}
	if reportedDropped == 0 {
		t.Fatal("queue of 4 absorbed 12 entries without drops: overflow untested")
	}
}
