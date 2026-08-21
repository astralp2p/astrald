package dir

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
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
