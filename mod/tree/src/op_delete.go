package tree

import (
	"github.com/astralp2p/astral-go/api/auth"
	treecli "github.com/astralp2p/astral-go/api/tree/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

func (mod *Module) OpDelete(ctx *astral.Context, q *routing.IncomingQuery, args treecli.DeleteArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	// note: the check precedes the split into plain and recursive deletion.
	if !mod.Auth.Authorize(ctx, &auth.ConfigureNodeStateAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	return treecli.NewNodeOps(mod.Root()).Delete(ctx, ch, args)
}
