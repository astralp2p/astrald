package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opNewArgs struct {
	Type string `query:"required"`
	In   string
	Out  string
}

// why: a zero value of a named type reads no node secret and changes no state; the type-registry leak is accepted (G9).
func (mod *Module) OpNew(ctx *astral.Context, query *routing.IncomingQuery, args opNewArgs) (err error) {
	ch := query.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	object := astral.New(args.Type)
	if object == nil {
		return ch.Send(&astral.Nil{})
	}

	return ch.Send(object)
}
