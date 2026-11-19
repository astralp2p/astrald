package kcp

import (
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"net"
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
}

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

// Read reads data from the KCP session.
// The first successful Read clears the deadline WrapKCPConn set.
func (c *WrappedConn) Read(p []byte) (int, error) {
	n, err := c.UDPSession.Read(p)
	if err == nil && c.answered.CompareAndSwap(false, true) {
		_ = c.UDPSession.SetDeadline(time.Time{})
	}

	return n, err
}

// Close closes the KCP session and, for a dialed conn, the UDP socket it owns.
//
// why: kcp-go closes a socket passed to NewConn only when it created it, so
// without this every dialed conn leaves its local port bound.
func (c *WrappedConn) Close() error {
	err := c.UDPSession.Close()
	if c.udpConn != nil {
		_ = c.udpConn.Close()
	}
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

	return &WrappedConn{
		UDPSession: sess,
		remote:     remote,
		local:      local,
		outbound:   outbound,
	}
}
