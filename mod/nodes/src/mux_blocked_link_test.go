package nodes

import (
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"

	"github.com/astralp2p/astrald/mod/nodes/frames"
)

// blockingCredit returns an onRead that blocks until release is closed, as a
// credit frame write blocks on a link whose transport is full, and a channel
// that receives once onRead has been entered.
func blockingCredit() (onRead func(int), entered <-chan struct{}, release chan struct{}) {
	in := make(chan struct{}, 1)
	release = make(chan struct{})
	onRead = func(int) {
		select {
		case in <- struct{}{}:
		default:
		}
		<-release
	}
	return onRead, in, release
}

// pushReturns reports whether push returns within a second.
func pushReturns(t *testing.T, push func() error) bool {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- push() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("push: %v", err)
		}
		return true
	case <-time.After(time.Second):
		return false
	}
}

// TestInputBufferPushWhileCreditBlocks: a Push, which the link's read loop
// runs, completes while a Read's credit frame is blocked on the link.
func TestInputBufferPushWhileCreditBlocks(t *testing.T) {
	onRead, entered, release := blockingCredit()
	defer close(release)

	buf := NewInputBuffer(1024, onRead)
	if err := buf.Push([]byte("one")); err != nil {
		t.Fatal(err)
	}
	go func() {
		p := make([]byte, 8)
		_, _ = buf.Read(p)
	}()
	<-entered

	if !pushReturns(t, func() error { return buf.Push([]byte("two")) }) {
		t.Fatal("Push blocked behind a Read whose credit frame was blocked")
	}
}

// TestSessionReaderPushWhileCreditBlocks: the same through the session
// reader, whose lock the link's read loop takes in Push.
func TestSessionReaderPushWhileCreditBlocks(t *testing.T) {
	onRead, entered, release := blockingCredit()
	defer close(release)

	r := newSessionReader(NewInputBuffer(1024, onRead))
	if err := r.Push([]byte("one")); err != nil {
		t.Fatal(err)
	}
	go func() {
		p := make([]byte, 8)
		_, _ = r.Read(p)
	}()
	<-entered

	if !pushReturns(t, func() error { return r.Push([]byte("two")) }) {
		t.Fatal("Push blocked behind a Read whose credit frame was blocked")
	}
}

// TestSessionWriterControlWhileWriteBlocks: Grow and PeerClose, which the
// link's read loop runs, complete while a Write is blocked on the link, and
// Pause waits for that Write.
func TestSessionWriterControlWhileWriteBlocks(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	buf := NewOutputBuffer(func([]byte) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return nil
	})
	buf.Grow(1024)
	w := newSessionWriter(buf, nil)

	go func() { _, _ = w.Write([]byte("data")) }()
	<-entered

	if !pushReturns(t, func() error { w.Grow(16); return nil }) {
		t.Fatal("Grow blocked behind a Write blocked on the link")
	}

	paused := make(chan struct{})
	go func() { w.Pause(); close(paused) }()
	select {
	case <-paused:
		t.Fatal("Pause returned while a Write was in progress")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case <-paused:
	case <-time.After(time.Second):
		t.Fatal("Pause did not return after the Write ended")
	}

	if !pushReturns(t, func() error { return w.PeerClose() }) {
		t.Fatal("PeerClose blocked")
	}
}

// TestMuxAnswersWithoutBlockingTheReadLoop: the frames the read loop answers
// with, a pong and a Reset for an unknown session, return while the link's
// write is blocked (a net.Pipe nobody reads).
func TestMuxAnswersWithoutBlockingTheReadLoop(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()
	defer local.Close()
	m := &Mux{ch: channel.New(local, channel.WithLockedWrites())}

	answered := []frames.Frame{
		&frames.Ping{Nonce: astral.NewNonce()},
		&frames.Read{Nonce: astral.NewNonce(), Len: 1},
		&frames.Data{Nonce: astral.NewNonce(), Payload: []byte("x")},
	}
	for _, f := range answered {
		if !pushReturns(t, func() error { return m.Handle(f) }) {
			t.Fatalf("Handle(%T) blocked on the link's write", f)
		}
	}
}
