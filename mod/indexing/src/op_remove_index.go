package indexing

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opRemoveIndexArgs struct {
	Nonce astral.Nonce
	In    string `query:"optional"`
	Out   string `query:"optional"`
}

func (mod *Module) OpRemoveIndex(ctx *astral.Context, q *routing.IncomingQuery, args opRemoveIndexArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	err := mod.RemoveIndexer(ctx, args.Nonce)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
