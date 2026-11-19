package gateway

import (
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"io"

	"github.com/astralp2p/astral-go/api/exonet"
)

var _ exonetmod.Conn = (*gatewayConn)(nil)
var _ exonetmod.KeepaliveConn = (*gatewayConn)(nil)

// gatewayConn wraps any io.ReadWriteCloser with gateway endpoint metadata.
type gatewayConn struct {
	io.ReadWriteCloser
	local     exonet.Endpoint
	remote    exonet.Endpoint
	outbound  bool
	keepalive exonetmod.LinkKeepalive
}

func (c *gatewayConn) LocalEndpoint() exonet.Endpoint  { return c.local }
func (c *gatewayConn) RemoteEndpoint() exonet.Endpoint { return c.remote }
func (c *gatewayConn) Outbound() bool                  { return c.outbound }

func (c *gatewayConn) LinkKeepalive() exonetmod.LinkKeepalive { return c.keepalive }

func newGatewayConn(conn exonetmod.Conn, local, remote exonet.Endpoint, keepalive exonetmod.LinkKeepalive) *gatewayConn {
	return &gatewayConn{ReadWriteCloser: conn, local: local, remote: remote, keepalive: keepalive}
}
