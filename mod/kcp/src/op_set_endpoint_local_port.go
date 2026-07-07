package kcp

import (
	"github.com/cryptopunkscc/astral-go/api/kcp"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opSetEndpointLocalPort struct {
	Endpoint  string
	LocalPort astral.Uint16
	Replace   astral.Bool `query:"optional"`
	In        string      `query:"optional"`
	Out       string      `query:"optional"`
}

func (mod *Module) OpSetEndpointLocalPort(ctx *astral.Context, q *routing.IncomingQuery, args opSetEndpointLocalPort) (err error) {
	endpoint, err := kcp.ParseEndpoint(args.Endpoint)
	if err != nil {
		return q.RejectWithCode(4)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	err = mod.SetEndpointLocalSocket(*endpoint, args.LocalPort, args.Replace)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&astral.Ack{})
}
