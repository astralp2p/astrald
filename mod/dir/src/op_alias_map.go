package dir

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
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
