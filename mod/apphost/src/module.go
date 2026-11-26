package apphost

import (
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	"net"
	"sync"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/debug"
	"github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/crypto"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/nodes"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/sys"
	"github.com/astralp2p/astrald/mod/tree"
	"github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/mod/vfs"
)

var _ apphostmod.Module = &Module{}

type Deps struct {
	Auth    auth.Module
	Crypto  crypto.Module
	Dir     dir.Module
	Objects objects.Module
	Sys     sys.Module
	Tree    tree.Module
	VFS     vfs.Module
}

type OptionalDeps struct {
	User  user.Module
	Nodes nodes.Module
}

type Module struct {
	Deps
	OptionalDeps
	ctx    *astral.Context
	config Config
	node   astral.Node
	log    *log.Logger
	db     *DB
	router routing.OpRouter

	listeners             []net.Listener
	conns                 <-chan net.Conn
	ipcHandlers           sig.Set[*IPCHandler]
	serviceHandlers       sig.Set[*ServiceHandler]
	bindSessions          bindRegistry
	sessionEnds           endSpacer
	enRoute               sig.Map[astral.Nonce, *queryEnRoute]
	pendingInboundQueries sig.Map[astral.Nonce, *pendingInboundQuery]

	// contractRequests files app ContractRequests and carries out their
	// Approvals. It is nil on a node without the user module;
	// apphost.request_contract answers ErrContractRequestsOff there.
	contractRequests *contractRequests
}

func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx.IncludeZone(astral.ZoneNetwork)

	var wg sync.WaitGroup
	var workerCount = mod.config.Workers

	mod.conns = mod.listen(ctx)
	mod.log.Log("gate: tokenless ops %v, fresh ops %v, unclaimed ops %v, claimed ops %v",
		mod.config.Gate.Tokenless, mod.config.Gate.States.Fresh,
		mod.config.Gate.States.Unclaimed, mod.config.Gate.States.Claimed)

	// spawn workers
	mod.log.Logv(2, "spawning %v workers", workerCount)
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func(i int) {
			defer debug.SaveLog(debug.SigInt)

			defer wg.Done()
			if err := mod.worker(ctx); err != nil {
				mod.log.Error("[worker:%v] error: %v", i, err)
			}
		}(i)
	}

	if mod.contractRequests != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mod.contractRequests.Run(mod.ctx, mod.config.ContractRequestPoll, func(err error) {
				mod.log.Logv(1, "contract requests: %v", err)
			})
		}()
	}

	// start the object server
	objectServer := NewHTTPServer(mod)
	objectServer.Run(ctx)

	wg.Wait()

	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

// EnRouteQueryExtras returns a copy of the Extra map of a guest query that is
// still en route (launched but not yet accepted or rejected). Once the query
// resolves the entry is gone and the result is nil.
func (mod *Module) EnRouteQueryExtras(nonce astral.Nonce) map[string]any {
	er, ok := mod.enRoute.Get(nonce)
	if !ok {
		return nil
	}
	return er.query.Extra.Clone()
}

func (mod *Module) String() string {
	return apphostmod.ModuleName
}

func (mod *Module) RoutingPriority() int {
	return astral.RoutingPriorityHigh
}
