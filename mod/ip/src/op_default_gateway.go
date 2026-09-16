package ip

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opDefaultGatewayArgs struct {
	Out string
}

func (mod *Module) OpDefaultGateway(ctx *astral.Context, q *routing.IncomingQuery, args opDefaultGatewayArgs) (err error) {
	if !mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	gw, err := mod.DefaultGateway()
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&gw)
}
