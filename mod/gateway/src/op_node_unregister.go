package gateway

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
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
