package nodes

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"github.com/astralp2p/astrald/mod/nodes/frames"
)

// stallTimeout is the ping timeout of the links in the write watchdog tests.
const stallTimeout = 300 * time.Millisecond

// stallSlack is how late past its deadline a watchdog effect may land.
const stallSlack = 500 * time.Millisecond

// newStallLink starts a link over a pipe with ping timeout timeout and no
// keepalive probe within the test's run.
func newStallLink(t *testing.T, timeout time.Duration) (*Link, net.Conn) {
	t.Helper()
	local, remote := net.Pipe()
	t.Cleanup(func() { local.Close(); remote.Close() })
	k := exonetmod.LinkKeepalive{Idle: time.Minute, Timeout: timeout}
	link := newLink(&Module{log: log.New(astral.Anyone)}, keepaliveConn{pipeIdentityConn{local}, k}, astral.NewNonce(), true)
	t.Cleanup(func() { link.Close() })
	return link, remote
}

// isStuckProbeCause reports whether err is the cause of death of a link whose
// probe write never left: the keepalive's ping timeout or the write watchdog.
// why either: both fire at the link's ping timeout, so the scheduler picks the first.
func isStuckProbeCause(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "ping timeout") || errors.Is(err, nodesmod.ErrWriteStalled))
}

// sendQueued starts n writers on link and returns the channel each one's Send result arrives on.
func sendQueued(link *Link, n int) <-chan error {
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { results <- link.Send(&frames.Reset{Nonce: astral.NewNonce()}) }()
	}
	return results
}

// waitErr polls link.Err until it is set or deadline passes, and returns it.
func waitErr(link *Link, deadline time.Time) error {
	for time.Now().Before(deadline) {
		if err := link.Err(); err != nil {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return link.Err()
}

// waitWriters waits for n results on results until deadline and fails on a writer that has not returned.
func waitWriters(t *testing.T, results <-chan error, n int, deadline time.Time) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-results:
		case <-time.After(time.Until(deadline)):
			t.Fatalf("%v of %v blocked writers still blocked at the deadline", n-i, n)
		}
	}
}

// TestWriteStallClosesWithoutNotice: the watchdog closes a link whose writer
// holds the write lock mid-frame. The link sends no close notice on a write
// stall, so it closes at once; the stuck writer and every writer queued behind
// it return within the ping timeout plus slack, without closeNoticeBound.
func TestWriteStallClosesWithoutNotice(t *testing.T) {
	link, remote := newStallLink(t, stallTimeout)

	// why one byte: net.Pipe is unbuffered, so the writer holds the write lock
	// mid-frame from here on, and the peer reads nothing more.
	stuck := make(chan error, 1)
	go func() { stuck <- link.Send(&frames.Data{Nonce: astral.NewNonce(), Payload: make([]byte, 1024)}) }()
	if _, err := remote.Read(make([]byte, 1)); err != nil {
		t.Fatalf("reading the first byte of the stuck frame: %v", err)
	}
	start := time.Now()
	queued := sendQueued(link, 3)

	if err := waitErr(link, start.Add(stallTimeout+stallSlack)); !errors.Is(err, nodesmod.ErrWriteStalled) {
		t.Fatalf("link error %v at %v after the stall; want %v within %v", err, time.Since(start), nodesmod.ErrWriteStalled, stallTimeout+stallSlack)
	}
	closeBy := start.Add(stallTimeout + stallSlack)
	waitDone(t, link, time.Until(closeBy), "link after the write stall")
	select {
	case err := <-stuck:
		if err == nil {
			t.Fatal("the stuck writer reported success after the close")
		}
	case <-time.After(time.Until(closeBy)):
		t.Fatal("the stuck writer did not return after the close")
	}
	waitWriters(t, queued, 3, closeBy)
}

// TestLinkClosesWhenWriteStalls: a peer that sends continuously but never
// reads stalls the pong the link writes back. The link closes with
// ErrWriteStalled within the ping timeout plus slack, and the writers blocked
// behind the stalled write return within the same bound.
func TestLinkClosesWhenWriteStalls(t *testing.T) {
	link, remote := newStallLink(t, stallTimeout)
	peer := channel.New(remote)

	start := time.Now()
	go func() {
		for peer.Send(&frames.Ping{Nonce: astral.NewNonce()}) == nil {
		}
	}()
	queued := sendQueued(link, 3)

	if err := waitErr(link, start.Add(stallTimeout+stallSlack)); !errors.Is(err, nodesmod.ErrWriteStalled) {
		t.Fatalf("link error %v at %v; want %v within %v", err, time.Since(start), nodesmod.ErrWriteStalled, stallTimeout+stallSlack)
	}
	closeBy := start.Add(stallTimeout + stallSlack)
	waitDone(t, link, time.Until(closeBy), "link with a peer that never reads")
	waitWriters(t, queued, 3, closeBy)
}

// TestLinkSlowReaderNotClosed: a peer that reads one frame per half the ping
// timeout keeps the link open while the writes together take several ping
// timeouts.
func TestLinkSlowReaderNotClosed(t *testing.T) {
	link, remote := newStallLink(t, stallTimeout)
	peer := channel.New(remote)
	go func() {
		for {
			if _, err := peer.Receive(); err != nil {
				return
			}
			time.Sleep(stallTimeout / 2)
		}
	}()

	const frameCount = 8
	start := time.Now()
	for i := 0; i < frameCount; i++ {
		if err := link.Send(&frames.Data{Nonce: astral.NewNonce(), Payload: make([]byte, 64)}); err != nil {
			t.Fatalf("send %v of %v after %v: %v", i+1, frameCount, time.Since(start), err)
		}
	}
	if took := time.Since(start); took < 2*stallTimeout {
		t.Fatalf("the writes took %v; want over %v so a timer over all of them would fire", took, 2*stallTimeout)
	}
	if err := link.Err(); err != nil {
		t.Fatalf("slow reader's link closed with %v; want open", err)
	}
}

// keepaliveNetConn is a conn that reports a remote endpoint and sets its own keepalive, as a transport's conn does.
type keepaliveNetConn struct {
	networkedConn
	k exonetmod.LinkKeepalive
}

func (c keepaliveNetConn) LinkKeepalive() exonetmod.LinkKeepalive { return c.k }

// TestWatchedConnKeepsKeepaliveAndNetwork: the link writes through the
// watchdog wrapper while link.conn stays the transport's conn, so the
// transport's keepalive and network still reach the link.
func TestWatchedConnKeepsKeepaliveAndNetwork(t *testing.T) {
	local, remote := net.Pipe()
	t.Cleanup(func() { local.Close(); remote.Close() })
	k := exonetmod.LinkKeepalive{Idle: 7 * time.Second, Timeout: 3 * time.Second}
	conn := keepaliveNetConn{networkedConn{pipeIdentityConn{local}, mustTCP(t, "127.0.0.1:1791")}, k}
	link := newLink(&Module{log: log.New(astral.Anyone)}, conn, astral.NewNonce(), true)
	t.Cleanup(func() { link.Close() })

	if link.conn != astral.Conn(conn) {
		t.Fatalf("link.conn is %T; want the transport's conn", link.conn)
	}
	if got := keepaliveOf(link.Network(), link.conn); got != k {
		t.Fatalf("keepaliveOf(link.conn) = %+v; want %+v", got, k)
	}
	if link.Network() != "tcp" || link.idlePing != k.Idle || link.pingTimeout != k.Timeout {
		t.Fatalf("link network %v timing %v/%v; want tcp %v/%v", link.Network(), link.idlePing, link.pingTimeout, k.Idle, k.Timeout)
	}
	w, ok := link.Channel.Transport().(*watchedConn)
	if !ok || w.Conn != astral.Conn(conn) {
		t.Fatalf("link writes through %T; want a watchedConn over the transport's conn", link.Channel.Transport())
	}
}

// stepClock is a clock a test moves by hand.
type stepClock struct{ t time.Time }

func (f *stepClock) now() time.Time { return f.t }

// newClockedWatch returns a watchedConn on a fake clock that starts at a fixed
// date far from the wall clock, so its decisions use only the fake clock, and
// a counter of onStall calls. The timeout is long enough that the real timer
// never fires within the test.
func newClockedWatch(t *testing.T) (*watchedConn, *stepClock, *int) {
	t.Helper()
	clock := &stepClock{t: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}
	stalls := new(int)
	c := &watchedConn{timeout: time.Hour, onStall: func() { *stalls++ }, now: clock.now}
	c.base = c.now()
	c.wdog = time.AfterFunc(c.timeout, c.check)
	c.wdog.Stop()
	t.Cleanup(func() { c.wdog.Stop() })
	return c, clock, stalls
}

// The elapsed time of a Write is the distance of the clock from the offset the
// Write stored, whatever the clock's absolute value.
func TestWatchedConnElapsedUsesOffsetFromBase(t *testing.T) {
	c, clock, _ := newClockedWatch(t)

	since := c.offset() + 1
	if since != 1 {
		t.Fatalf("offset stored at base = %d; want 1 (0 means no Write in flight)", since)
	}
	if got := c.elapsed(since); got != 0 {
		t.Fatalf("elapsed at start = %v; want 0", got)
	}
	clock.t = clock.t.Add(90 * time.Minute)
	if got := c.elapsed(since); got != 90*time.Minute {
		t.Fatalf("elapsed after 90 min = %v; want 90m", got)
	}
}

// check re-arms for the rest of the timeout while the Write in flight is younger
// than the timeout, and calls onStall once it is not.
func TestWatchedConnCheckFollowsTheClock(t *testing.T) {
	c, clock, stalls := newClockedWatch(t)

	c.since.Store(c.offset() + 1)
	clock.t = clock.t.Add(c.timeout - time.Minute)
	c.check()
	if *stalls != 0 {
		t.Fatalf("onStall called %d times at timeout-1m; want 0", *stalls)
	}
	if !c.wdog.Stop() {
		t.Fatal("watchdog is not re-armed after a check of a young Write")
	}

	clock.t = clock.t.Add(time.Minute)
	c.check()
	if *stalls != 1 {
		t.Fatalf("onStall called %d times at timeout; want 1", *stalls)
	}

	c.since.Store(0)
	c.check()
	if *stalls != 1 {
		t.Fatalf("onStall called %d times with no Write in flight; want still 1", *stalls)
	}
}

// discardConn is a conn whose Write returns at once.
type discardConn struct{ pipeIdentityConn }

func (discardConn) Write(p []byte) (int, error) { return len(p), nil }

// The Write path allocates nothing.
func TestWatchedConnWriteDoesNotAllocate(t *testing.T) {
	c := newWatchedConn(discardConn{}, time.Hour, func() {})
	t.Cleanup(func() { c.wdog.Stop() })
	p := []byte{1}
	if allocs := testing.AllocsPerRun(1000, func() { c.Write(p) }); allocs != 0 {
		t.Fatalf("Write allocates %v times; want 0", allocs)
	}
}
