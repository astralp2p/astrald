package apphost

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/tree"
)

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	if err = core.Inject(mod.node, &mod.Deps); err != nil {
		return
	}

	// why NoLocal: a failed save must fail the setter, not fall back to memory.
	mod.policy.AppRegister.NoLocal = true
	if err = tree.BindPath(ctx, &mod.policy, mod.Tree.Root(), policyPath, true); err != nil {
		return
	}

	// optional — apphost can run without user module
	core.Inject(mod.node, &mod.OptionalDeps)
	return
}
