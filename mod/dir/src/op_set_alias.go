package dir

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetAliasArgs struct {
	ID    *astral.Identity `query:"required"`
	Alias *string          `query:"required"` // required but can be empty
	Out   string
}

func (mod *Module) OpSetAlias(ctx *astral.Context, q *routing.IncomingQuery, args opSetAliasArgs) (err error) {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	err = mod.SetAlias(args.ID, *args.Alias)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
