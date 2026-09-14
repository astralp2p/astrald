package indexing

import (
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
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

	// why: a failed cleanup leaves registrations nobody reaches, which is no reason
	// to refuse loading the module.
	deleted, cleanupErr := mod.deleteUnownedIndexers(ctx)
	if cleanupErr != nil {
		mod.log.Log("error deleting unowned indexers: %v", cleanupErr)
	}
	if deleted > 0 {
		mod.log.Logv(1, "deleted %v unowned indexer registrations", deleted)
	}

	return nil
}
