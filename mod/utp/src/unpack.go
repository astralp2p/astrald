package utp

import (
	"bytes"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astrald/mod/utp"
)

var _ exonetmod.Unpacker = &Module{}

func (mod *Module) Unpack(network string, data []byte) (exonet.Endpoint, error) {
	switch network {
	case "utp":
	default:
		return nil, exonetmod.ErrUnsupportedNetwork
	}
	return Unpack(data)
}

// Unpack deserializes a binary-encoded uTP endpoint from buf.
func Unpack(buf []byte) (e *utp.Endpoint, err error) {
	e = &utp.Endpoint{}
	_, err = e.ReadFrom(bytes.NewReader(buf))
	return
}
