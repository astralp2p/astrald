package mcp

import (
	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	mcpmod "github.com/astralp2p/astrald/mod/mcp"
)

var _ mcpmod.Module = &Module{}

type Module struct {
	Deps
	ctx    *astral.Context
	config Config
	node   astral.Node
	log    *log.Logger
	db     *DB
	router routing.OpRouter

	// tools are the deployment's own, read once at load — declared_tools.go.
	tools []declaredTool
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

// Agents answers every agent whose participant mod/messaging still names, and
// drops the row of every other — see dropWithdrawn.
func (mod *Module) Agents() (list []*mcp.Agent, err error) {
	rows, err := mod.db.ListAgents()
	if err != nil {
		return nil, err
	}
	list = make([]*mcp.Agent, 0, len(rows))
	for i := range rows {
		r := &rows[i]

		dropped, err := mod.dropWithdrawn(r)
		if err != nil {
			return nil, err
		}
		if dropped {
			continue
		}

		list = append(list, &mcp.Agent{
			Identity:  r.Identity,
			Alias:     astral.String8(r.Alias),
			Token:     astral.String8(r.Token),
			ExpiresAt: astral.Time(r.ExpiresAt),
		})
	}
	return list, nil
}

func (mod *Module) String() string {
	return mcpmod.ModuleName
}
