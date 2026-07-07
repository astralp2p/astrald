package apphost

/*
	op cancel cancels an en route query
*/

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opCancelArgs struct {
	ID    astral.Nonce
	Cause *string `query:"optional"`
	Out   string  `query:"optional"`
}

func (mod *Module) OpCancel(ctx *astral.Context, q *routing.IncomingQuery, args opCancelArgs) (err error) {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	enRoute, found := mod.enRoute.Get(args.ID)
	if !found {
		return ch.Send(astral.NewError("query not found"))
	}

	if args.Cause == nil || len(*args.Cause) == 0 {
		enRoute.cancel(nil)
	} else {
		enRoute.cancel(astral.NewError(*args.Cause))
	}

	mod.log.Logv(2, "cancelled query %v", args.ID)

	return ch.Send(&astral.Ack{})
}
