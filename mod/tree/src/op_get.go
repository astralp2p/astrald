package tree

import (
	"github.com/astralp2p/astral-go/api/auth"
	treecli "github.com/astralp2p/astral-go/api/tree/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

func (mod *Module) OpGet(ctx *astral.Context, q *routing.IncomingQuery, args treecli.GetArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// note: a path under a remote mount is walked on the remote node, so the check
	// also keeps a refused caller from making this node query another one.
	if !mod.Auth.Authorize(ctx, &auth.SeeNodeStateAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	return treecli.NewNodeOps(mod.Root()).Get(ctx, ch, args)
}
