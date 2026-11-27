package auth

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/crypto"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/tree"
	usermod "github.com/astralp2p/astrald/mod/user"
)

type Deps struct {
	Crypto  crypto.Module
	Dir     dir.Module
	Objects objects.Module
	Tree    tree.Module
}

// OptionalDeps holds the user module. Without it the node counts as claimed.
//
// note: auth reads only the claim state (isUnclaimed). The walk reads nothing
// from the user module; the user module answers for the user's powers through
// the handlers it registers.
type OptionalDeps struct {
	User usermod.Module
}

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	if err = core.Inject(mod.node, &mod.Deps); err != nil {
		return
	}

	// why NoLocal: a failed save must fail the caller, not fall back to memory.
	mod.authorizers.NoLocal = true
	if err = mod.authorizers.BindPath(ctx, mod.Tree.Root(), authorizersPath, true); err != nil {
		return
	}

	if err = mod.restoreAuthorizers(ctx, mod.configured); err != nil {
		return
	}

	// note: auth runs without the user module; the node then counts as claimed.
	_ = core.Inject(mod.node, &mod.OptionalDeps)

	return
}
