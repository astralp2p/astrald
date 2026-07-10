package nat

import (
	"fmt"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/nat"
	"github.com/astralp2p/astrald/mod/tree"
)

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return err
	}

	moduleSettingsPath := fmt.Sprintf(`/mod/%s/settings`, nat.ModuleName)
	err = tree.BindPath(ctx, &mod.settings, mod.Tree.Root(), moduleSettingsPath, true)
	if err != nil {
		return fmt.Errorf("nat module: bind settings: %w", err)
	}

	return nil
}
