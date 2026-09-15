package nodes

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opCloseLinkArgs struct {
	LinkID astral.Nonce `query:"required"`
	Out    string
}

// OpCloseLink closes the link with the given link id.
func (mod *Module) OpCloseLink(ctx *astral.Context, q *routing.IncomingQuery, args opCloseLinkArgs) (err error) {
	if !mod.authorizeAdminNetwork(ctx, q) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	err = mod.CloseLink(args.LinkID)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}
	return ch.Send(&astral.Ack{})
}
