package utp

import (
	"github.com/cryptopunkscc/astral-go/api/exonet"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
	"github.com/cryptopunkscc/astrald/mod/utp"
)

func (mod *Module) Parse(network string, address string) (exonet.Endpoint, error) {
	switch network {
	case "utp":
	default:
		return nil, exonetmod.ErrUnsupportedNetwork
	}

	return utp.ParseEndpoint(address)
}
