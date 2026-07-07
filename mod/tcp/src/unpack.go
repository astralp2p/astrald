package tcp

import (
	"bytes"
	"github.com/cryptopunkscc/astral-go/api/exonet"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
	"github.com/cryptopunkscc/astrald/mod/tcp"
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
