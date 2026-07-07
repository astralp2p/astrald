package nodes

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opCloseLinkArgs struct {
	ID  astral.Nonce
	Out string `query:"optional"`
}

// OpCloseLink closes a link with the given id.
func (mod *Module) OpCloseLink(ctx *astral.Context, q *routing.IncomingQuery, args opCloseLinkArgs) (err error) {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	err = mod.CloseLink(args.ID)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}
	return ch.Send(&astral.Ack{})
}
