package objects

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opGetTypeArgs struct {
	ID  *astral.ObjectID
	Out string `query:"optional"`
}

func (mod *Module) OpGetType(ctx *astral.Context, q *routing.IncomingQuery, args opGetTypeArgs) (err error) {
	ctx = ctx.WithIdentity(q.Caller())

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	t, err := mod.GetType(ctx, args.ID)
	if err != nil {
		return ch.Send(astral.NewError("unknown type"))
	}

	return ch.Send((*astral.String8)(&t))
}
