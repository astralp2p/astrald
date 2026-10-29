package nodes

import (
	"github.com/astralp2p/astral-go/api/auth"
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
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if !mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	err = mod.CloseLink(args.LinkID)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}
	return ch.Send(&astral.Ack{})
}
