package tree

import (
	"github.com/astralp2p/astral-go/api/auth"
	treecli "github.com/astralp2p/astral-go/api/tree/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

func (mod *Module) OpDelete(ctx *astral.Context, q *routing.IncomingQuery, args treecli.DeleteArgs) (err error) {
	// note: the check precedes the split into plain and recursive deletion.
	if !mod.Auth.Authorize(ctx, &auth.ConfigureNodeStateAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	return treecli.NewNodeOps(mod.Root()).Delete(ctx, q, args)
}
