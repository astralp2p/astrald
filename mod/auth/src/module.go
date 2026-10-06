package auth

import (
	"sync"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/resources"
)

var _ auth.Module = &Module{}

type Module struct {
	Deps
	OptionalDeps
	config   Config
	node     astral.Node
	log      *log.Logger
	assets   resources.Resources
	db       *DB
	router   routing.OpRouter
	handlers sig.Map[string, []auth.Handler]
	indexMu  sync.Mutex

	// handlersMu serializes Add's read-modify-write of handlers against
	// other Adds and against get, so no registration is lost.
	handlersMu sync.RWMutex

	// external holds at most one authorizer per action type; a second for the
	// same type is refused where it is added.
	external sig.Map[string, *ExternalAuthorizer]
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
	mod.handlersMu.Lock()
	defer mod.handlersMu.Unlock()

	for _, h := range handlers {
		t := h.ActionType()
		old, _ := mod.handlers.Get(t)
		mod.handlers.Replace(t, append(old, h))
	}
}

// get returns the handlers for an action type. The lock covers only the
// lookup; callers run the handlers without holding it.
func (mod *Module) get(actionType string) []auth.Handler {
	mod.handlersMu.RLock()
	defer mod.handlersMu.RUnlock()

	h, _ := mod.handlers.Get(actionType)
	return h
}
