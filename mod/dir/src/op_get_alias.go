package dir

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opGetAliasArgs struct {
	ID  *astral.Identity `query:"required"`
	Out string
}

func (mod *Module) OpGetAlias(ctx *astral.Context, q *routing.IncomingQuery, args opGetAliasArgs) (err error) {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	alias, err := mod.GetAlias(args.ID)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send((*astral.String8)(&alias))
}
