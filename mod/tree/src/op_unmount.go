package tree

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opUnmountArgs struct {
	Path string `query:"required"`
	In   string
	Out  string
}

func (mod *Module) OpUnmount(ctx *astral.Context, q *routing.IncomingQuery, args opUnmountArgs) (err error) {
	// why: the check sits at the op and not in Module.Unmount, because Unmount
	// also serves in-process callers, which carry no query caller to authorize.
	if !mod.Auth.Authorize(ctx, &auth.ConfigureNodeStateAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if err := mod.Unmount(args.Path); err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
