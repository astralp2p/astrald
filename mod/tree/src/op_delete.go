package tree

import (
	treecli "github.com/astralp2p/astral-go/api/tree/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

func (mod *Module) OpDelete(ctx *astral.Context, q *routing.IncomingQuery, args treecli.DeleteArgs) (err error) {
	// note: the check precedes the split into plain and recursive deletion.
	if !mod.authorizeConfigureNodeState(ctx, q) {
		return q.Reject()
	}

	return treecli.NewNodeOps(mod.Root()).Delete(ctx, q, args)
}
