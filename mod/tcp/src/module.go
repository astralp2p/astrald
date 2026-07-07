package tcp

import (
	"context"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
	tcpmod "github.com/cryptopunkscc/astrald/mod/tcp"
	"sync"
	"time"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/api/nodes"
	"github.com/cryptopunkscc/astral-go/api/tcp"
	"github.com/cryptopunkscc/astral-go/api/tree"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

var _ tcpmod.Module = &Module{}

type Module struct {
	Deps
	config   Config
	settings Settings

	node            astral.Node
	log             *log.Logger
	ctx             *astral.Context
	configEndpoints []exonet.Endpoint
	router          routing.OpRouter

	mu sync.Mutex

	server             sig.Switch
	ephemeralListeners sig.Map[astral.Uint16, exonetmod.EphemeralListener]
}

// Settings holds live, tree-bound toggles for the module; values may change at runtime
// without a restart and are observed via Follow.
type Settings struct {
	Listen *tree.Value[*astral.Bool] `tree:"listen"`
	Dial   *tree.Value[*astral.Bool] `tree:"dial"`
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return tcpmod.ModuleName
}

func (mod *Module) acceptAll(ctx context.Context, conn exonetmod.Conn) (shouldStop bool, err error) {
	err = mod.Nodes.EstablishInboundLink(ctx, conn)
	if err != nil {
		return false, err
	}

	return false, nil
}

func (mod *Module) Run(ctx *astral.Context) (err error) {
	mod.ctx = ctx

	err = mod.syncConfig(ctx)
	if err != nil {
		return err
	}

	go func() {
		for v := range mod.settings.Listen.Follow(ctx) {
			mod.server.Set(ctx, v == nil || bool(*v), mod.startServer)
		}
	}()

	<-ctx.Done()

	return nil
}

func (mod *Module) ListenPort() int {
	return mod.config.ListenPort
}

func (mod *Module) endpoints() (list []*nodes.EndpointWithTTL) {
	ips, _ := mod.IP.LocalIPs()
	for _, tip := range ips {
		list = append(list, nodes.NewEndpointWithTTL(&tcp.Endpoint{
			IP:   tip,
			Port: astral.Uint16(mod.config.ListenPort),
		}, 7*24*time.Hour))
	}

	for _, e := range mod.configEndpoints {
		list = append(list, nodes.NewEndpointWithTTL(e, 7*24*time.Hour))
	}

	return list
}

func (mod *Module) syncConfig(ctx *astral.Context) error {
	if mod.config.Dial != nil {
		val := astral.Bool(*mod.config.Dial)
		if err := mod.settings.Dial.Set(ctx, &val); err != nil {
			return err
		}
	}

	if mod.config.Listen != nil {
		val := astral.Bool(*mod.config.Listen)
		if err := mod.settings.Listen.Set(ctx, &val); err != nil {
			return err
		}
	}

	return nil
}
