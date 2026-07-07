package indexing

import (
	"context"
	treemod "github.com/cryptopunkscc/astrald/mod/tree"
	"sync"

	"github.com/cryptopunkscc/astral-go/api/tree"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/lib/routing"
	"github.com/cryptopunkscc/astral-go/sig"
	"github.com/cryptopunkscc/astrald/mod/indexing"
	"github.com/cryptopunkscc/astrald/mod/objects"
	"github.com/cryptopunkscc/astrald/resources"
)

var _ indexing.Module = &Module{}

type Deps struct {
	Objects objects.Module
	Tree    treemod.Module
}

type Module struct {
	Deps
	config   Config
	node     astral.Node
	log      *log.Logger
	assets   resources.Resources
	router   routing.OpRouter
	db       *DB
	ctx      *astral.Context
	repos    tree.Node
	indexers tree.Node

	syncing sig.Map[string, context.CancelFunc]

	notifyMu sync.Mutex
	notify   chan struct{}
}

func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx

	sub, err := mod.repos.Sub(ctx)
	if err != nil {
		return err
	}

	for repoName := range sub {
		err = mod.startRepoSync(repoName)
		if err != nil {
			mod.log.Logv(1, "error starting repo sync: %v", err)
		}
	}

	<-ctx.Done()
	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return indexing.ModuleName
}
