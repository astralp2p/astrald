package utp

import (
	"github.com/cryptopunkscc/astral-go/api/exonet"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
	"github.com/cryptopunkscc/utp"
)

// WrappedConn adapts a raw utp.Conn to the exonet.Conn interface, carrying
// pre-resolved endpoints and a direction flag so callers never re-parse addresses.
type WrappedConn struct {
	*utp.Conn
	remote   exonet.Endpoint
	local    exonet.Endpoint
	outbound bool
}

func (w WrappedConn) Outbound() bool {
	return w.outbound
}

func (w WrappedConn) LocalEndpoint() exonet.Endpoint {
	return w.local
}

func (w WrappedConn) RemoteEndpoint() exonet.Endpoint {
	return w.remote
}

func WrapUtpConn(
	conn *utp.Conn,
	remote exonet.Endpoint,
	local exonet.Endpoint,
	outbound bool) exonetmod.Conn {
	return WrappedConn{
		outbound: outbound,
		Conn:     conn,
		remote:   remote,
		local:    local,
	}
}
