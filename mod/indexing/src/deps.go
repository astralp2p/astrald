package indexing

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/mod/tree"
)

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	mod.repos, err = tree.Query(ctx, mod.Tree.Root(), "/mod/indexing/repos", true)
	if err != nil {
		return err
	}

	mod.indexers, err = tree.Query(ctx, mod.Tree.Root(), "/mod/indexing/indexers", true)
	if err != nil {
		return err
	}

	return err
}
