package kcp

import (
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	kcpgo "github.com/xtaci/kcp-go/v5"
)

// Ensure *WrappedConn implements exonet.Conn
var _ exonetmod.Conn = (*WrappedConn)(nil)

// WrappedConn wraps a KCP session and provides
// connection-like semantics at the exonet layer.
type WrappedConn struct {
	*kcpgo.UDPSession
	remote      exonet.Endpoint
	local       exonet.Endpoint
	outbound    bool
	connected   atomic.Bool
	dialTimeout time.Duration
}

// Outbound reports whether this connection was initiated locally.
func (w WrappedConn) Outbound() bool {
	return w.outbound
}

// LocalEndpoint returns the local endpoint of the connection.
func (w WrappedConn) LocalEndpoint() exonet.Endpoint {
	return w.local
}

// RemoteEndpoint returns the remote endpoint of the connection.
func (w WrappedConn) RemoteEndpoint() exonet.Endpoint {
	return w.remote
}

// Read reads data from the KCP session.
// Until the first successful I/O, a dial timeout is enforced.
func (c *WrappedConn) Read(p []byte) (int, error) {
	if !c.connected.Load() {
		_ = c.UDPSession.SetDeadline(time.Now().Add(c.dialTimeout))
	}

	n, err := c.UDPSession.Read(p)
	if err == nil {
		c.connected.Store(true)
		// Clear deadline after first successful I/O
		_ = c.UDPSession.SetDeadline(time.Time{})
	}

	return n, err
}

// Write writes data to the KCP session.
// Until the first successful I/O, a dial timeout is enforced.
func (c *WrappedConn) Write(p []byte) (int, error) {
	if !c.connected.Load() {
		_ = c.UDPSession.SetDeadline(time.Now().Add(c.dialTimeout))
	}

	n, err := c.UDPSession.Write(p)
	if err == nil {
		c.connected.Store(true)
		// Clear deadline after first successful I/O
		_ = c.UDPSession.SetDeadline(time.Time{})
	}

	return n, err
}

// WrapKCPConn wraps a kcp-go UDPSession as an exonet.Conn.
func WrapKCPConn(
	sess *kcpgo.UDPSession,
	remote exonet.Endpoint,
	local exonet.Endpoint,
	outbound bool,
	dialTimeout time.Duration,
) exonetmod.Conn {
	return &WrappedConn{
		UDPSession:  sess,
		remote:      remote,
		local:       local,
		outbound:    outbound,
		dialTimeout: dialTimeout,
	}
}
