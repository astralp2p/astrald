package vfs

import (
	"context"
	"sync"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/user"
	vfsmod "github.com/astralp2p/astrald/mod/vfs"
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

	// computeLocks holds one compute lock per VFS, keyed by the identity string. See lockCompute.
	computeLocks sig.Map[string, *sync.Mutex]

	clock      clock
	sched      *dirScheduler
	gate       *dirGate
	passBudget int // dirPassBudget; a test sets a smaller one

	// ctx is the context of the directory stores and deletes. It ends when Run ends.
	// why: it exists from Load, because a query can reach the module before Run starts.
	ctx    *astral.Context
	cancel context.CancelFunc
}

// setupDirectories makes the state of the directory stores on a clock.
func (mod *Module) setupDirectories(clk clock) {
	mod.clock = clk
	mod.sched = newDirScheduler(mod.config)
	mod.gate = newDirGate()
	mod.passBudget = dirPassBudget
	mod.ctx, mod.cancel = astral.NewContext(nil).WithCancel()
}

// seedPending makes every VFS whose root ObjectID is cleared pending, with a full bucket.
func (mod *Module) seedPending() error {
	ids, err := mod.db.listPendingVolumes()
	if err != nil {
		return err
	}

	mod.sched.seed(ids, mod.clock.Now())

	return nil
}

// Run runs the background compute and the sweeper until ctx ends.
func (mod *Module) Run(ctx *astral.Context) error {
	stop := context.AfterFunc(ctx, mod.cancel)
	defer stop()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		mod.runDirWorker(mod.ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		mod.runDirSweeper(mod.ctx)
	}()

	wg.Wait()

	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return vfsmod.ModuleName
}
