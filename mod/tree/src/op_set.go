package tree

import (
	treecli "github.com/cryptopunkscc/astral-go/api/tree/client"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

func (mod *Module) OpSet(ctx *astral.Context, q *routing.IncomingQuery, args treecli.SetArgs) (err error) {
	return treecli.NewNodeOps(mod.Root()).Set(ctx, q, args)
}
