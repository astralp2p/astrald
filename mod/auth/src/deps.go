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

// claimState is the part of the user module auth reads: whether the node has a
// user yet (IsNodeClaim, isUnclaimed).
//
// why declared here and not mod/user's Module: auth depends on no more of the
// user module than it reads. core.Inject matches the "user" module by field
// name and assignability, so the narrow interface injects as well.
//
// note: the walk reads nothing from the user module. The user module answers
// for the user's powers through the handlers it registers.
type claimState interface {
	Ready() <-chan struct{}
	Identity() *astral.Identity
}

// why: LoadDependencies ignores the OptionalDeps injection error, so a user
// module that stopped satisfying claimState would leave User nil and every node
// would count as claimed; this fails the build instead.
var _ claimState = (usermod.Module)(nil)

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
