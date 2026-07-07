package nat

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opSetEnabledArgs struct {
	Arg bool   `query:"optional"`
	In  string `query:"optional"`
	Out string `query:"optional"`
}

func (mod *Module) OpSetEnabled(ctx *astral.Context, q *routing.IncomingQuery, args opSetEnabledArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	val := astral.Bool(args.Arg)
	mod.settings.Enabled.Set(ctx, &val)

	return ch.Send(&astral.Ack{})
}
