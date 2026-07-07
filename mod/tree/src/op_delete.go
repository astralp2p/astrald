package tree

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/lib/routing"
	treecli "github.com/cryptopunkscc/astrald/mod/tree/client"
)

func (mod *Module) OpDelete(ctx *astral.Context, q *routing.IncomingQuery, args treecli.DeleteArgs) (err error) {
	return treecli.NewNodeOps(mod.Root()).Delete(ctx, q, args)
}
