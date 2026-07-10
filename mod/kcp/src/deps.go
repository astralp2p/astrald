package kcp

import (
	"fmt"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/exonet"
	"github.com/astralp2p/astrald/mod/ip"
	"github.com/astralp2p/astrald/mod/kcp"
	"github.com/astralp2p/astrald/mod/nearby"
	"github.com/astralp2p/astrald/mod/nodes"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/tree"
)

type Deps struct {
	Exonet  exonet.Module
	Nodes   nodes.Module
	Nearby  nearby.Module
	Objects objects.Module
	IP      ip.Module
	Tree    tree.Module
}

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	moduleSettingsPath := fmt.Sprintf(`/mod/%s/settings`, kcp.ModuleName)

	err = tree.BindPath(ctx, &mod.settings, mod.Tree.Root(), moduleSettingsPath, true)
	if err != nil {
		return fmt.Errorf("kcp module: bind settings: %w", err)
	}

	mod.Exonet.SetDialer("kcp", mod)
	mod.Exonet.SetParser("kcp", mod)
	mod.Exonet.SetUnpacker("kcp", mod)
	mod.Nodes.AddResolver(mod)

	return
}
