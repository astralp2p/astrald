package indexing

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opUnregisterIndexerArgs struct {
	Nonce astral.Nonce `query:"required"`
	In    string
	Out   string
}

func (mod *Module) OpUnregisterIndexer(ctx *astral.Context, q *routing.IncomingQuery, args opUnregisterIndexerArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	err := mod.UnregisterIndexer(ctx, args.Nonce)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
