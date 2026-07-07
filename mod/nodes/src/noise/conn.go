package noise

import (
	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/brontide"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
)

var _ exonetmod.Conn = &Conn{}

// Conn is a net.SecureConn authenticated and ecrypted via the noise_xk protocol
type Conn struct {
	conn     exonetmod.Conn
	brontide *brontide.Conn
}

func (conn *Conn) Read(p []byte) (n int, err error) {
	return conn.brontide.Read(p)
}

func (conn *Conn) Write(p []byte) (n int, err error) {
	return conn.brontide.Write(p)
}

func (conn *Conn) Close() error {
	return conn.brontide.Close()
}

func (conn *Conn) Outbound() bool {
	return conn.conn.Outbound()
}

func (conn *Conn) LocalEndpoint() exonet.Endpoint {
	return conn.conn.LocalEndpoint()
}

func (conn *Conn) RemoteEndpoint() exonet.Endpoint {
	return conn.conn.RemoteEndpoint()
}

func (conn *Conn) LocalIdentity() *astral.Identity {
	return astral.IdentityFromPubKey(conn.brontide.LocalPub())
}

func (conn *Conn) RemoteIdentity() *astral.Identity {
	return astral.IdentityFromPubKey(conn.brontide.RemotePub())
}
