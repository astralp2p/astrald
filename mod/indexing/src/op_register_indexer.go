package indexing

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRegisterIndexerArgs struct {
	Name string
	In   string `query:"optional"`
	Out  string `query:"optional"`
}

func (mod *Module) OpRegisterIndexer(ctx *astral.Context, q *routing.IncomingQuery, args opRegisterIndexerArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	nonce, err := mod.RegisterIndexer(ctx, args.Name)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&nonce)
}
