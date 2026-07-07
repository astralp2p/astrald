package auth

import (
	"sync"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astral-go/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/resources"
)

var _ auth.Module = &Module{}

type Module struct {
	Deps
	config   Config
	node     astral.Node
	log      *log.Logger
	assets   resources.Resources
	db       *DB
	router   routing.OpRouter
	handlers sig.Map[string, []auth.Handler]
	indexMu  sync.Mutex
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return auth.ModuleName
}

func (mod *Module) Run(ctx *astral.Context) error {
	go mod.indexer(ctx)
	<-ctx.Done()
	return nil
}

// Add registers typed handlers; the action type is inferred from each handler.
func (mod *Module) Add(handlers ...auth.TypedHandler) {
	for _, h := range handlers {
		t := h.ActionType()
		mod.handlers.Replace(t, append(mod.get(t), h))
	}
}

func (mod *Module) get(actionType string) []auth.Handler {
	h, _ := mod.handlers.Get(actionType)
	return h
}
