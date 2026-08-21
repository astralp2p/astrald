package tcp

import (
	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/tcp"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

func (mod *Module) Parse(network string, address string) (exonet.Endpoint, error) {
	switch network {
	case "tcp", "inet":
	default:
		return nil, exonetmod.ErrUnsupportedNetwork
	}

	return tcp.ParseEndpoint(address)
}
