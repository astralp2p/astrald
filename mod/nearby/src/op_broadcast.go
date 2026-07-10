package nearby

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opBroadcastArgs struct {
	Out string `query:"optional"`
}

func (mod *Module) OpBroadcast(ctx *astral.Context, q *routing.IncomingQuery, args opBroadcastArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	err = mod.Broadcast()
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&astral.Ack{})
}
