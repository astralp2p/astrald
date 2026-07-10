package nodes

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opResolveEndpointsArgs struct {
	ID  string
	Out string `query:"optional"`
}

func (mod *Module) OpResolveEndpoints(ctx *astral.Context, q *routing.IncomingQuery, args opResolveEndpointsArgs) (err error) {
	targetID, err := mod.Dir.ResolveIdentity(args.ID)
	if err != nil {
		return q.RejectWithCode(2)
	}

	endpoints, err := mod.ResolveEndpoints(ctx.WithIdentity(q.Caller()), targetID)
	if err != nil {
		mod.log.Error("resolve endpoints: %v", err)
		return q.RejectWithCode(astral.CodeInternalError)
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	for endpoint := range endpoints {
		err = ch.Send(endpoint)
		if err != nil {
			return ch.Send(astral.NewError(err.Error()))
		}
	}

	return ch.Send(&astral.EOS{})
}
