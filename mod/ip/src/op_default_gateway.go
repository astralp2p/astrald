package ip

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opDefaultGatewayArgs struct {
	Out string `query:"optional"`
}

func (mod *Module) OpDefaultGateway(ctx *astral.Context, q *routing.IncomingQuery, args opDefaultGatewayArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	gw, err := mod.DefaultGateway()
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&gw)
}
