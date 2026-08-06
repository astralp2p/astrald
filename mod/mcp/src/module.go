package mcp

import (
	"sync"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	mcpmod "github.com/astralp2p/astrald/mod/mcp"
)

var _ mcpmod.Module = &Module{}

type Module struct {
	Deps
	OptionalDeps
	ctx    *astral.Context
	config Config
	node   astral.Node
	log    *log.Logger
	db     *DB
	router routing.OpRouter

	listenMu  sync.Mutex
	listeners map[string]chan *session   // agent identity -> parked astral-listen; guarded by listenMu
	pending   map[string][]*pendingQuery // agent identity -> queued queries; guarded by listenMu

	agentIDs sig.Set[string]           // registered agent identities, mirrors mcp__agents
	sessions sig.Map[string, *session] // session id -> live dialog stream
}

func (mod *Module) Run(ctx *astral.Context) error {
	// why: agents may query remote identities, so the module context carries
	// the network zone like apphost's does.
	mod.ctx = ctx.IncludeZone(astral.ZoneNetwork)

	if err := NewMCPServer(mod).Run(ctx); err != nil {
		return err
	}

	<-ctx.Done()
	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) Agents() (list []*mcpmod.Agent, err error) {
	rows, err := mod.db.ListAgents()
	if err != nil {
		return nil, err
	}
	list = make([]*mcpmod.Agent, len(rows))
	for i, r := range rows {
		list[i] = &mcpmod.Agent{
			Identity:  r.Identity,
			Alias:     astral.String8(r.Alias),
			Token:     astral.String8(r.Token),
			ExpiresAt: astral.Time(r.ExpiresAt),
		}
	}
	return list, nil
}

func (mod *Module) String() string {
	return mcpmod.ModuleName
}
