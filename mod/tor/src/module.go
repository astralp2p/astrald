package tor

import (
	"github.com/cryptopunkscc/astral-go/api/tor"
	"github.com/cryptopunkscc/astral-go/api/tree"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/sig"
	"github.com/cryptopunkscc/astrald/core/assets"
	"github.com/cryptopunkscc/astrald/mod/exonet"
	"github.com/cryptopunkscc/astrald/mod/nearby"
	"github.com/cryptopunkscc/astrald/mod/nodes"
	treemod "github.com/cryptopunkscc/astrald/mod/tree"
	"golang.org/x/net/proxy"
)

const defaultListenPort = 1791

type Deps struct {
	Nodes  nodes.Module
	Exonet exonet.Module
	Nearby nearby.Module
	Tree   treemod.Module
}

type Settings struct {
	Listen *tree.Value[*astral.Bool] `tree:"listen"`
	Dial   *tree.Value[*astral.Bool] `tree:"dial"`
}

type Module struct {
	Deps
	config   Config
	settings Settings

	node      astral.Node
	assets    assets.Assets
	log       *log.Logger
	ctx       *astral.Context
	proxy     proxy.ContextDialer
	torServer *Server
	server    sig.Switch
}

func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx

	if err := mod.loadSettings(ctx); err != nil {
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

func (mod *Module) loadSettings(ctx *astral.Context) error {
	if mod.config.Dial != nil {
		val := astral.Bool(*mod.config.Dial)
		if err := mod.settings.Dial.Set(ctx, &val); err != nil {
			return err
		}
	}

	if mod.config.Listen != nil && !*mod.config.Listen {
		val := astral.Bool(*mod.config.Dial)
		if err := mod.settings.Listen.Set(ctx, &val); err != nil {
			return err
		}
	}

	return nil
}

func (mod *Module) String() string { return tor.ModuleName }
