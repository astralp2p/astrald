package auth

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/crypto"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/tree"
)

type Deps struct {
	Crypto  crypto.Module
	Dir     dir.Module
	Objects objects.Module
	Tree    tree.Module
}

// claimState is the part of the user module isNodeClaim reads: whether the
// node has a user yet.
//
// why declared here and not mod/user's Module: auth reads two of its methods
// and depends on no more of the user module. core.Inject matches the "user"
// module by field name and assignability, so the narrow interface injects as
// well.
type claimState interface {
	Ready() <-chan struct{}
	Identity() *astral.Identity
}

// OptionalDeps holds the user module. Without it the node counts as claimed.
type OptionalDeps struct {
	User claimState
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
