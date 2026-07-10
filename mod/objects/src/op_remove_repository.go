package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRemoveRepositoryArgs struct {
	Name string
	In   string `query:"optional"`
	Out  string `query:"optional"`
}

func (mod *Module) OpRemoveRepository(ctx *astral.Context, q *routing.IncomingQuery, args opRemoveRepositoryArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	err = mod.RemoveRepository(args.Name)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	return ch.Send(&astral.Ack{})
}
