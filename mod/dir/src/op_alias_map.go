package dir

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opAliasMapArgs struct {
	In  string
	Out string
}

func (mod *Module) OpAliasMap(ctx *astral.Context, q *routing.IncomingQuery, args opAliasMapArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	return ch.Send(mod.AliasMap())
}
