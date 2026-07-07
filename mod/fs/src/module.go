package fs

import (
	"os"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/lib/routing"
	"github.com/cryptopunkscc/astrald/core/assets"
	"github.com/cryptopunkscc/astrald/mod/fs"
)

var _ fs.Module = &Module{}

type Module struct {
	Deps
	config  Config
	node    astral.Node
	assets  assets.Assets
	log     *log.Logger
	db      *DB
	ctx     *astral.Context
	indexer *Indexer
	router  routing.OpRouter
}

func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx

	mod.indexer.startWorkers(ctx, 4)
	go func() {
		err := mod.indexer.init(ctx)
		if err != nil {
			mod.log.Error("indexer init error: %v", err)
		}
	}()

	<-ctx.Done()
	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return fs.ModuleName
}

func resolveFileID(path string) (*astral.ObjectID, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fileID, err := astral.Resolve(file)
	if err != nil {
		return nil, err
	}

	return fileID, nil
}
