package gateway

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opNodeUnregister struct {
	Out string `query:"optional"`
}

func (mod *Module) OpNodeUnregister(
	ctx *astral.Context,
	q *routing.IncomingQuery,
	args opNodeUnregister,
) error {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if err := mod.unregister(q.Caller()); err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&astral.Ack{})
}
