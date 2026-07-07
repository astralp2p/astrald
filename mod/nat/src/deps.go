package nat

import (
	"fmt"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/mod/nat"
	"github.com/cryptopunkscc/astrald/mod/tree"
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
