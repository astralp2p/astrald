package apphost

import (
	apphostmod "github.com/cryptopunkscc/astrald/mod/apphost"
	"net"
	"sync"

	"github.com/cryptopunkscc/astral-go/api/apphost"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/lib/routing"
	"github.com/cryptopunkscc/astral-go/sig"
	"github.com/cryptopunkscc/astrald/debug"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/mod/crypto"
	"github.com/cryptopunkscc/astrald/mod/dir"
	"github.com/cryptopunkscc/astrald/mod/objects"
	"github.com/cryptopunkscc/astrald/mod/user"
)

var _ apphostmod.Module = &Module{}

type Deps struct {
	Auth    auth.Module
	Crypto  crypto.Module
	Dir     dir.Module
	Objects objects.Module
}

type OptionalDeps struct {
	User user.Module
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
	wsHandlers            sig.Set[*WSHandler]
	enRoute               sig.Map[astral.Nonce, *queryEnRoute]
	pendingInboundQueries sig.Map[astral.Nonce, *pendingInboundQuery]
}

func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx.IncludeZone(astral.ZoneNetwork)

	var wg sync.WaitGroup
	var workerCount = mod.config.Workers

	mod.conns = mod.listen(ctx)

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

	// start the object server
	objectServer := NewHTTPServer(mod)
	objectServer.Run(ctx)

	wg.Wait()

	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) LocalApps() ([]*apphost.App, error) {
	rows, err := mod.db.ListLocalApps()
	if err != nil {
		return nil, err
	}
	list := make([]*apphost.App, len(rows))
	for i, r := range rows {
		list[i] = &apphost.App{AppID: r.AppID, HostID: r.HostID, InstalledAt: astral.Time(r.InstalledAt)}
	}
	return list, nil
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
