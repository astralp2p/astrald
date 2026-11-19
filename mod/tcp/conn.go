package tcp

import (
	"github.com/astralp2p/astral-go/api/tcp"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"net"

	"github.com/astralp2p/astral-go/api/exonet"
)

var _ exonetmod.Conn = Conn{}
var _ exonetmod.KeepaliveConn = Conn{}

// Conn is an exonet.Conn that wraps a net.Conn.
type Conn struct {
	net.Conn
	outbound       bool
	localEndpoint  *tcp.Endpoint
	remoteEndpoint *tcp.Endpoint
	keepalive      exonetmod.LinkKeepalive
}

// WrapConn returns an instance of Conn that wraps the given net.Conn; keepalive times the links over it.
func WrapConn(conn net.Conn, outbound bool, keepalive exonetmod.LinkKeepalive) *Conn {
	c := &Conn{
		Conn:      conn,
		outbound:  outbound,
		keepalive: keepalive,
	}

	c.localEndpoint, _ = tcp.ParseEndpoint(conn.LocalAddr().String())
	c.remoteEndpoint, _ = tcp.ParseEndpoint(conn.RemoteAddr().String())

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

func (conn Conn) LinkKeepalive() exonetmod.LinkKeepalive {
	return conn.keepalive
}
