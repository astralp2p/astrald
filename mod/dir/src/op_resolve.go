package dir

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opResolveArgs struct {
	Name string `query:"required"`
	Out  string
}

func (mod *Module) OpResolve(ctx *astral.Context, q *routing.IncomingQuery, args opResolveArgs) (err error) {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	id, err := mod.ResolveIdentity(args.Name)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(id)
}
