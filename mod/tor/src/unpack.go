package tor

import (
	"bytes"
	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/tor"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
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
