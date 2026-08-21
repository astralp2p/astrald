package kcp

import (
	"github.com/astralp2p/astral-go/api/exonet"
	kcpmod "github.com/astralp2p/astral-go/api/kcp"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

func (mod *Module) Parse(network string, address string) (exonet.Endpoint, error) {
	switch network {
	case "kcp":
	default:
		return nil, exonetmod.ErrUnsupportedNetwork
	}

	return kcpmod.ParseEndpoint(address)
}
