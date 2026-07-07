package kcp

import (
	"github.com/cryptopunkscc/astral-go/api/exonet"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
	kcpmod "github.com/cryptopunkscc/astrald/mod/kcp"
)

func (mod *Module) Parse(network string, address string) (exonet.Endpoint, error) {
	switch network {
	case "kcp":
	default:
		return nil, exonetmod.ErrUnsupportedNetwork
	}

	return kcpmod.ParseEndpoint(address)
}
