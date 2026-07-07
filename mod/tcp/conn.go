package tcp

import (
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
	"net"

	"github.com/cryptopunkscc/astral-go/api/exonet"
)

var _ exonetmod.Conn = Conn{}

// Conn is an exonet.Conn that wraps a net.Conn.
type Conn struct {
	net.Conn
	outbound       bool
	localEndpoint  *Endpoint
	remoteEndpoint *Endpoint
}

// WrapConn returns an instance of Conn that wraps the given net.Conn.
func WrapConn(conn net.Conn, outbound bool) *Conn {
	c := &Conn{
		Conn:     conn,
		outbound: outbound,
	}

	c.localEndpoint, _ = ParseEndpoint(conn.LocalAddr().String())
	c.remoteEndpoint, _ = ParseEndpoint(conn.RemoteAddr().String())

	return c
}

func (conn Conn) LocalEndpoint() exonet.Endpoint {
	return conn.localEndpoint
}

func (conn Conn) RemoteEndpoint() exonet.Endpoint {
	return conn.remoteEndpoint
}

func (conn Conn) Outbound() bool {
	return conn.outbound
}
