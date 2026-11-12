package vfs

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/user"
	vfsmod "github.com/astralp2p/astrald/mod/vfs"
	"sync"
)

var _ vfsmod.Module = &Module{}

type Deps struct {
	Auth    auth.Module
	Objects objects.Module
}

type OptionalDeps struct {
	User user.Module
}

type Module struct {
	Deps
	OptionalDeps
	config   Config
	node     astral.Node
	identity *astral.Identity // the identity of the node
	log      *log.Logger
	db       *DB
	router   routing.OpRouter

	// locks holds one lock per VFS, keyed by the identity string. See lockVFS.
	locks sig.Map[string, *sync.Mutex]
}

func (mod *Module) Run(ctx *astral.Context) error {
	<-ctx.Done()
	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return vfsmod.ModuleName
}
