package tcp

import (
	"fmt"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/exonet"
	ipmod "github.com/astralp2p/astrald/mod/ip"
	"github.com/astralp2p/astrald/mod/nearby"
	"github.com/astralp2p/astrald/mod/nodes"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/tcp"
	"github.com/astralp2p/astrald/mod/tree"
)

type Deps struct {
	Exonet  exonet.Module
	Nodes   nodes.Module
	Nearby  nearby.Module
	Objects objects.Module
	IP      ipmod.Module
	Tree    tree.Module
}

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	moduleSettingsPath := fmt.Sprintf(`/mod/%s/settings`, tcp.ModuleName)
	err = tree.BindPath(ctx, &mod.settings, mod.Tree.Root(), moduleSettingsPath, true)
	if err != nil {
		return fmt.Errorf("tcp module: bind settings: %w", err)
	}

	mod.Exonet.SetDialer("tcp", mod)
	mod.Exonet.SetParser("tcp", mod)
	mod.Exonet.SetUnpacker("tcp", mod)
	mod.Nodes.AddResolver(mod)

	return
}
