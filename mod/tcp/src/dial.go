package tcp

import (
	_net "net"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/exonet"
	"github.com/cryptopunkscc/astrald/mod/tcp"
)

func (mod *Module) Dial(ctx *astral.Context, endpoint exonet.Endpoint) (exonet.Conn, error) {
	switch endpoint.Network() {
	case "tcp", "inet":
	default:
		return nil, exonet.ErrUnsupportedNetwork
	}

	dial := mod.settings.Dial.Get()
	if dial != nil && !*dial {
		return nil, exonet.ErrDisabledNetwork
	}

	var dialer = _net.Dialer{Timeout: mod.config.DialTimeout, KeepAlive: 5 * time.Second}

	tcpConn, err := dialer.DialContext(ctx, "tcp", endpoint.Address())
	if err != nil {
		return nil, err
	}

	return tcp.WrapConn(tcpConn, true), nil
}
