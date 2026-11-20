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

// TestWriteStallCloseNoticeGivesUp: the watchdog closes a link whose writer
// holds the write lock mid-frame. The close notice queues behind that writer
// on the write lock, gives up within closeNoticeBound, and the link closes;
// the stuck writer and every writer queued behind it return.
func TestWriteStallCloseNoticeGivesUp(t *testing.T) {
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
	closeBy := start.Add(stallTimeout + closeNoticeBound + stallSlack)
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
// behind the stalled write return.
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
	closeBy := start.Add(stallTimeout + closeNoticeBound + stallSlack)
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
