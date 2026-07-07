package utp

import (
	"fmt"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/astral"
	utpmod "github.com/cryptopunkscc/astrald/mod/utp"
	"github.com/cryptopunkscc/utp"
)

var _ exonetmod.Dialer = &Module{}

// Dial establishes a reliable (utp) connection and wraps it as an exonet.Conn.
func (mod *Module) Dial(ctx *astral.Context, endpoint exonet.Endpoint) (
	c exonetmod.Conn, err error) {
	switch endpoint.Network() {
	case "utp":
	default:
		return nil, exonetmod.ErrUnsupportedNetwork
	}

	dialer := utp.Dialer{Timeout: mod.config.DialTimeout}
	conn, err := dialer.Dial("utp", endpoint.Address())
	if err != nil {
		return nil, fmt.Errorf(`utp module/dial dialing endpoint failed: %w`,
			err)
	}

	// Close raw conn on any subsequent error; noop on success.
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()

	localEndpoint, err := utpmod.ParseEndpoint(conn.LocalAddr().String())
	if err != nil {
		return nil, fmt.Errorf(`utp module/dial parsing local endpoint failed: %w`, err)
	}

	remoteEndpoint, err := utpmod.ParseEndpoint(conn.RemoteAddr().String())
	if err != nil {
		return nil, fmt.Errorf(`utp module/dial parsing remote endpoint failed: %w`, err)
	}

	return WrapUtpConn(conn, remoteEndpoint, localEndpoint, true), nil
}
