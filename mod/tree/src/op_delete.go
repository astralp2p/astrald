package tree

import (
	treecli "github.com/cryptopunkscc/astral-go/api/tree/client"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

func (mod *Module) OpDelete(ctx *astral.Context, q *routing.IncomingQuery, args treecli.DeleteArgs) (err error) {
	return treecli.NewNodeOps(mod.Root()).Delete(ctx, q, args)
}
