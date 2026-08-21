package tcp

import (
	"bytes"
	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/tcp"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

var _ exonetmod.Unpacker = &Module{}

func (mod *Module) Unpack(network string, data []byte) (exonet.Endpoint, error) {
	switch network {
	case "tcp", "inet":
	default:
		return nil, exonetmod.ErrUnsupportedNetwork
	}
	return Unpack(data)
}

// Unpack deserializes a TCP endpoint from its binary wire representation.
func Unpack(buf []byte) (e *tcp.Endpoint, err error) {
	e = &tcp.Endpoint{}
	_, err = e.ReadFrom(bytes.NewReader(buf))
	return
}
