package kcp

import (
	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListEndpointLocalMappingsArgs struct {
	In  string `query:"optional"`
	Out string `query:"optional"`
}

func (mod *Module) OpListEndpointLocalMappings(ctx *astral.Context, q *routing.IncomingQuery, args opListEndpointLocalMappingsArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	mappings := mod.GetEndpointsMappings()
	for k, v := range mappings {
		err = ch.Send(&kcp.EndpointLocalMapping{
			Address: k,
			Port:    v,
		})
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
