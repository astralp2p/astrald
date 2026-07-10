package exonet

import (
	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/dir"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"github.com/astralp2p/astrald/resources"
)

var _ exonetmod.Module = &Module{}

type Deps struct {
	Dir dir.Module
}

type Module struct {
	Deps
	config Config
	node   astral.Node
	log    *log.Logger
	assets resources.Resources

	dialers   sig.Map[string, exonetmod.Dialer]
	unpackers sig.Map[string, exonetmod.Unpacker]
	parser    sig.Map[string, exonetmod.Parser]
}

func (mod *Module) Run(ctx *astral.Context) error {
	<-ctx.Done()

	return nil
}

// Dial dispatches to the dialer registered for the endpoint's network; returns ErrUnsupportedNetwork if none is registered.
func (mod *Module) Dial(ctx *astral.Context, endpoint exonet.Endpoint) (conn exonetmod.Conn, err error) {
	d, found := mod.dialers.Get(endpoint.Network())
	if found {
		return d.Dial(ctx, endpoint)
	}

	return nil, exonetmod.ErrUnsupportedNetwork
}

// Unpack dispatches to the unpacker registered for the given network; returns ErrUnsupportedNetwork if none is registered.
func (mod *Module) Unpack(network string, data []byte) (exonet.Endpoint, error) {
	u, found := mod.unpackers.Get(network)
	if found {
		return u.Unpack(network, data)
	}

	return nil, exonetmod.ErrUnsupportedNetwork
}

// Parse dispatches to the parser registered for the given network; returns ErrUnsupportedNetwork if none is registered.
func (mod *Module) Parse(network string, address string) (exonet.Endpoint, error) {
	p, found := mod.parser.Get(network)
	if found {
		return p.Parse(network, address)
	}

	return nil, exonetmod.ErrUnsupportedNetwork

}

func (mod *Module) SetDialer(network string, dialer exonetmod.Dialer) {
	mod.dialers.Replace(network, dialer)
}

func (mod *Module) SetUnpacker(network string, unpacker exonetmod.Unpacker) {
	mod.unpackers.Replace(network, unpacker)
}

func (mod *Module) SetParser(network string, parser exonetmod.Parser) {
	mod.parser.Replace(network, parser)
}
