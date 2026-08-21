package tree

import (
	treecli "github.com/astralp2p/astral-go/api/tree/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

func (mod *Module) OpGet(ctx *astral.Context, q *routing.IncomingQuery, args treecli.GetArgs) (err error) {
	return treecli.NewNodeOps(mod.Root()).Get(ctx, q, args)
}
