package tree

import (
	treecli "github.com/astralp2p/astral-go/api/tree/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

func (mod *Module) OpSet(ctx *astral.Context, q *routing.IncomingQuery, args treecli.SetArgs) (err error) {
	// note: the check precedes the split into single-value and batch mode.
	if !mod.authorizeConfigureNodeState(ctx, q) {
		return q.Reject()
	}

	return treecli.NewNodeOps(mod.Root()).Set(ctx, q, args)
}
