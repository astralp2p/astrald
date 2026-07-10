package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opNewArgs struct {
	Type string
	In   string `query:"optional"`
	Out  string `query:"optional"`
}

func (mod *Module) OpNew(ctx *astral.Context, query *routing.IncomingQuery, args opNewArgs) (err error) {
	ch := query.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	object := astral.New(args.Type)
	if object == nil {
		return ch.Send(&astral.Nil{})
	}

	return ch.Send(object)
}
