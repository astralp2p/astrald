package tree

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/lib/routing"
	treecli "github.com/cryptopunkscc/astrald/mod/tree/client"
)

func (mod *Module) OpSet(ctx *astral.Context, q *routing.IncomingQuery, args treecli.SetArgs) (err error) {
	return treecli.NewNodeOps(mod.Root()).Set(ctx, q, args)
}
