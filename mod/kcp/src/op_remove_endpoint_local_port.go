package kcp

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/kcp"
)

type opRemoveEndpointLocalPort struct {
	Endpoint astral.String8
	In       string `query:"optional"`
	Out      string `query:"optional"`
}

func (mod *Module) OpRemoveEndpointLocalPort(ctx *astral.Context, q *routing.IncomingQuery, args opRemoveEndpointLocalPort) (err error) {
	endpoint, err := kcp.ParseEndpoint(string(args.Endpoint))
	if err != nil {
		return q.RejectWithCode(4)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	err = mod.RemoveEndpointLocalSocket(*endpoint)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&astral.Ack{})
}
