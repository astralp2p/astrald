package kcp

import (
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	kcpgo "github.com/xtaci/kcp-go/v5"
)

// Ensure *WrappedConn implements exonet.Conn
var _ exonetmod.Conn = (*WrappedConn)(nil)
var _ exonetmod.KeepaliveConn = (*WrappedConn)(nil)

// WrappedConn wraps a KCP session and provides
// connection-like semantics at the exonet layer.
type WrappedConn struct {
	*kcpgo.UDPSession
	// udpConn is the socket Dial created for this session; nil for inbound
	// sessions, whose socket belongs to the listener.
	udpConn  *net.UDPConn
	remote   exonet.Endpoint
	local    exonet.Endpoint
	outbound bool
	// answered turns true at the first successful Read, the first proof that
	// the peer is alive.
	answered  atomic.Bool
	keepalive exonetmod.LinkKeepalive

	mu       sync.Mutex       // serializes Read callers
	asks     chan int         // the sizes of the session Reads that Read asks readSession to run
	results  chan sessionRead // the reader's answers; buffer 1
	closed   atomic.Bool
	closing  chan struct{} // closed by Close
	released chan struct{} // closed when the session closes
}

// sessionRead is the outcome of one Read the reader ran on the session.
type sessionRead struct {
	p   []byte
	err error
}

// closeLinger is how long Close keeps the session of a conn whose peer
// answered, so the session delivers the data written before Close.
const closeLinger = time.Second

// Outbound reports whether this connection was initiated locally.
func (c *WrappedConn) Outbound() bool {
	return c.outbound
}

// LinkKeepalive returns the keepalive timing of links over this connection.
func (c *WrappedConn) LinkKeepalive() exonetmod.LinkKeepalive {
	return c.keepalive
}

// LocalEndpoint returns the local endpoint of the connection.
func (c *WrappedConn) LocalEndpoint() exonet.Endpoint {
	return c.local
}

// RemoteEndpoint returns the remote endpoint of the connection.
func (c *WrappedConn) RemoteEndpoint() exonet.Endpoint {
	return c.remote
}

// Read reads data from the KCP session through the reader goroutine.
// The first successful Read clears the deadline WrapKCPConn set.
// Close releases a blocked Read; a Read after Close fails with io.ErrClosedPipe.
//
// why the reader: kcp-go releases a blocked Read only at its own Close or at
// a deadline, and a deadline set while a Read runs races with that Read
// inside kcp-go; Close lingers before kcp-go's Close.
func (c *WrappedConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() {
		return 0, io.ErrClosedPipe
	}

	select {
	case c.asks <- len(p):
	case <-c.closing:
		return 0, io.ErrClosedPipe
	}
	select {
	case r := <-c.results:
		if r.err == nil && c.answered.CompareAndSwap(false, true) {
			_ = c.UDPSession.SetDeadline(time.Time{})
		}
		return copy(p, r.p), r.err
	case <-c.closing:
		return 0, io.ErrClosedPipe
	}
}

// readSession runs the session Reads that Read asks for, into its own buffer, until the session closes.
// why its own buffer: a Read that Close released no longer owns its buffer.
func (c *WrappedConn) readSession() {
	var buf []byte
	for {
		select {
		case n := <-c.asks:
			if cap(buf) < n {
				buf = make([]byte, n)
			}
			m, err := c.UDPSession.Read(buf[:n])
			c.results <- sessionRead{p: buf[:m], err: err}
		case <-c.released:
			return
		}
	}
}

// Write queues p on the KCP session. A Write after Close fails with io.ErrClosedPipe.
func (c *WrappedConn) Write(p []byte) (int, error) {
	if c.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	return c.UDPSession.Write(p)
}

// Close ends the conn's reads at once and fails later writes. The session
// and, for a dialed conn, its UDP socket close closeLinger later when the peer
// answered, at once otherwise. A second Close fails with io.ErrClosedPipe.
//
// why the linger: kcp-go drops at its own Close every segment its congestion
// window still holds back or the peer has not acknowledged, and a new
// session's window opens one segment per acknowledgment; a frame written
// right before Close, such as the link close frame (mod/nodes link.go
// CloseWithError), would never reach the peer.
// why only an answered peer: a peer that never answered acknowledges nothing.
// note: a Write blocked on a full send window returns when the linger ends.
// note: a dialed conn holds its local port until the linger ends.
func (c *WrappedConn) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return io.ErrClosedPipe
	}
	close(c.closing)
	if !c.answered.Load() {
		return c.release()
	}
	time.AfterFunc(closeLinger, func() { _ = c.release() })
	return nil
}

// release closes the KCP session and, for a dialed conn, the UDP socket it owns.
//
// why: kcp-go closes a socket passed to NewConn only when it created it, so
// without this every dialed conn leaves its local port bound.
func (c *WrappedConn) release() error {
	err := c.UDPSession.Close()
	if c.udpConn != nil {
		_ = c.udpConn.Close()
	}
	close(c.released)
	return err
}

// WrapKCPConn wraps a kcp-go UDPSession as an exonet.Conn. Until the first
// successful Read, a Read, or a Write that blocks, fails once dialTimeout has
// passed since the wrap. A zero dialTimeout sets no deadline.
//
// why: kcp-go queues a Write with no peer behind the address, so only a Read
// proves the peer is alive; without the deadline a handshake with a dead
// address waits for the caller's context.
func WrapKCPConn(
	sess *kcpgo.UDPSession,
	remote exonet.Endpoint,
	local exonet.Endpoint,
	outbound bool,
	dialTimeout time.Duration,
) *WrappedConn {
	if dialTimeout > 0 {
		_ = sess.SetDeadline(time.Now().Add(dialTimeout))
	}

	c := &WrappedConn{
		UDPSession: sess,
		remote:     remote,
		local:      local,
		outbound:   outbound,
		asks:       make(chan int),
		results:    make(chan sessionRead, 1),
		closing:    make(chan struct{}),
		released:   make(chan struct{}),
	}
	go c.readSession()

	return c
}
