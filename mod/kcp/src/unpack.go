package kcp

import (
	"bytes"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"

	"github.com/astralp2p/astral-go/api/exonet"
	kcpmod "github.com/astralp2p/astral-go/api/kcp"
)

var _ exonetmod.Unpacker = &Module{}

func (mod *Module) Unpack(network string, data []byte) (exonet.Endpoint, error) {
	switch network {
	case "kcp":
	default:
		return nil, exonetmod.ErrUnsupportedNetwork
	}
	return Unpack(data)
}

func Unpack(buf []byte) (e *kcpmod.Endpoint, err error) {
	e = &kcpmod.Endpoint{}
	_, err = e.ReadFrom(bytes.NewReader(buf))
	return
}
