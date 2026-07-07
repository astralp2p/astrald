package gateway

import (
	"bytes"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/gateway"
)

func (mod *Module) Unpack(network string, data []byte) (exonet.Endpoint, error) {
	if network != NetworkName {
		return nil, exonetmod.ErrUnsupportedNetwork
	}
	return Unpack(data)
}

// Unpack converts a binary representation of the address to a struct
func Unpack(data []byte) (addr *gateway.Endpoint, err error) {
	addr = &gateway.Endpoint{}
	_, err = astral.Objectify(addr).ReadFrom(bytes.NewReader(data))
	return
}
