package tor

import (
	"bytes"
	"github.com/cryptopunkscc/astral-go/api/exonet"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
	"github.com/cryptopunkscc/astrald/mod/tor"
)

const addrVersion = 3

var _ exonetmod.Unpacker = &Module{}

func (mod *Module) Unpack(network string, data []byte) (exonet.Endpoint, error) {
	if network != tor.ModuleName {
		return nil, exonetmod.ErrUnsupportedNetwork
	}
	return Unpack(data)
}

// Unpack converts a binary representation of the address to a struct
func Unpack(data []byte) (_ *tor.Endpoint, err error) {
	var e tor.Endpoint

	_, err = e.ReadFrom(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	return &e, nil
}
