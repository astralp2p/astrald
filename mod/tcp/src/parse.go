package tcp

import (
	"github.com/cryptopunkscc/astral-go/api/exonet"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
	"github.com/cryptopunkscc/astrald/mod/tcp"
)

func (mod *Module) Parse(network string, address string) (exonet.Endpoint, error) {
	switch network {
	case "tcp", "inet":
	default:
		return nil, exonetmod.ErrUnsupportedNetwork
	}

	return tcp.ParseEndpoint(address)
}
