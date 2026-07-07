package gateway

import (
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
	"io"

	"github.com/cryptopunkscc/astral-go/api/exonet"
)

var _ exonetmod.Conn = (*gatewayConn)(nil)

// gatewayConn wraps any io.ReadWriteCloser with gateway endpoint metadata.
type gatewayConn struct {
	io.ReadWriteCloser
	local    exonet.Endpoint
	remote   exonet.Endpoint
	outbound bool
}

func (c *gatewayConn) LocalEndpoint() exonet.Endpoint  { return c.local }
func (c *gatewayConn) RemoteEndpoint() exonet.Endpoint { return c.remote }
func (c *gatewayConn) Outbound() bool                  { return c.outbound }

func newGatewayConn(conn exonetmod.Conn, local, remote exonet.Endpoint) *gatewayConn {
	return &gatewayConn{ReadWriteCloser: conn, local: local, remote: remote}
}
