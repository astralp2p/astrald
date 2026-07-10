package tor

import (
	"fmt"

	"github.com/astralp2p/astral-go/api/tor"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/tree"
)

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	moduleSettingsPath := fmt.Sprintf(`/mod/%s/settings`, tor.ModuleName)

	err = tree.BindPath(ctx, &mod.settings, mod.Tree.Root(), moduleSettingsPath, true)
	if err != nil {
		return fmt.Errorf("tor module: bind settings: %w", err)
	}

	mod.Exonet.SetDialer("tor", mod)
	mod.Exonet.SetParser("tor", mod)
	mod.Exonet.SetUnpacker("tor", mod)
	mod.Nodes.AddResolver(mod)

	return nil
}
