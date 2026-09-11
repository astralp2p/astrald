package nat

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetEnabledArgs struct {
	Arg bool
	In  string
	Out string
}

func (mod *Module) OpSetEnabled(ctx *astral.Context, q *routing.IncomingQuery, args opSetEnabledArgs) (err error) {
	if !mod.authorizeAdminNetwork(ctx, q) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	val := astral.Bool(args.Arg)
	mod.settings.Enabled.Set(ctx, &val)

	return ch.Send(&astral.Ack{})
}
