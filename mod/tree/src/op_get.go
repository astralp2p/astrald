package tree

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/routing"
	treecli "github.com/cryptopunkscc/astrald/mod/tree/client"
)

func (mod *Module) OpGet(ctx *astral.Context, q *routing.IncomingQuery, args treecli.GetArgs) (err error) {
	return treecli.NewNodeOps(mod.Root()).Get(ctx, q, args)
}
