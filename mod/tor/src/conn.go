package tor

import (
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
	"net"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/api/tor"
)

// Type check
var _ exonetmod.Conn = &Conn{}

// Conn represents a network connection over Tor
type Conn struct {
	net.Conn
	remoteEndpoint *tor.Endpoint
	outbound       bool
}

// LocalEndpoint returns an empty address, since there is no local endpoint in Tor
func (conn *Conn) LocalEndpoint() exonet.Endpoint {
	return &tor.Endpoint{}
}

// RemoteEndpoint returns the address of the remote party
func (conn *Conn) RemoteEndpoint() exonet.Endpoint {
	if conn.remoteEndpoint == nil || conn.remoteEndpoint.IsZero() {
		return nil // return a generic nil instead of (*tor.Endpoint)(nil)
	}

	return conn.remoteEndpoint
}

// Outbound returns true if the connection is outbound
func (conn *Conn) Outbound() bool {
	return conn.outbound
}

// newConn wraps a standard net.Conn into a astral's net.Conn with the addition of boundness
func newConn(conn net.Conn, remoteEndpoint *tor.Endpoint, outbound bool) exonetmod.Conn {
	return &Conn{
		Conn:           conn,
		remoteEndpoint: remoteEndpoint,
		outbound:       outbound,
	}
}
