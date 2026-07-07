package tree

import (
	treecli "github.com/cryptopunkscc/astral-go/api/tree/client"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

func (mod *Module) OpList(ctx *astral.Context, q *routing.IncomingQuery, args treecli.ListArgs) (err error) {
	return treecli.NewNodeOps(mod.Root()).List(ctx, q, args)
}
