package user

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opSyncWithArgs struct {
	Node  *astral.Identity
	Start astral.Uint64 `query:"optional"`
	Out   string        `query:"optional"`
}

// OpSyncWith triggers an outbound asset sync with the specified node over the network zone.
func (mod *Module) OpSyncWith(ctx *astral.Context, q *routing.IncomingQuery, args opSyncWithArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	err = mod.syncAssets(ctx.IncludeZone(astral.ZoneNetwork), args.Node)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&astral.Ack{})
}
