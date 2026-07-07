package exonet

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/mod/dir"
	"github.com/cryptopunkscc/astrald/mod/exonet"
	"github.com/cryptopunkscc/astrald/resources"
)

var _ exonet.Module = &Module{}

type Deps struct {
	Dir dir.Module
}

type Module struct {
	Deps
	config Config
	node   astral.Node
	log    *log.Logger
	assets resources.Resources

	dialers   sig.Map[string, exonet.Dialer]
	unpackers sig.Map[string, exonet.Unpacker]
	parser    sig.Map[string, exonet.Parser]
}

func (mod *Module) Run(ctx *astral.Context) error {
	<-ctx.Done()

	return nil
}

// Dial dispatches to the dialer registered for the endpoint's network; returns ErrUnsupportedNetwork if none is registered.
func (mod *Module) Dial(ctx *astral.Context, endpoint exonet.Endpoint) (conn exonet.Conn, err error) {
	d, found := mod.dialers.Get(endpoint.Network())
	if found {
		return d.Dial(ctx, endpoint)
	}

	return nil, exonet.ErrUnsupportedNetwork
}

// Unpack dispatches to the unpacker registered for the given network; returns ErrUnsupportedNetwork if none is registered.
func (mod *Module) Unpack(network string, data []byte) (exonet.Endpoint, error) {
	u, found := mod.unpackers.Get(network)
	if found {
		return u.Unpack(network, data)
	}

	return nil, exonet.ErrUnsupportedNetwork
}

// Parse dispatches to the parser registered for the given network; returns ErrUnsupportedNetwork if none is registered.
func (mod *Module) Parse(network string, address string) (exonet.Endpoint, error) {
	p, found := mod.parser.Get(network)
	if found {
		return p.Parse(network, address)
	}

	return nil, exonet.ErrUnsupportedNetwork

}

func (mod *Module) SetDialer(network string, dialer exonet.Dialer) {
	mod.dialers.Replace(network, dialer)
}

func (mod *Module) SetUnpacker(network string, unpacker exonet.Unpacker) {
	mod.unpackers.Replace(network, unpacker)
}

func (mod *Module) SetParser(network string, parser exonet.Parser) {
	mod.parser.Replace(network, parser)
}
