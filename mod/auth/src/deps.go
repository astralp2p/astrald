package auth

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/crypto"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/objects"
)

type Deps struct {
	Crypto  crypto.Module
	Dir     dir.Module
	Objects objects.Module
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

func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	if err = core.Inject(mod.node, &mod.Deps); err != nil {
		return
	}

	// note: auth runs without the user module; the node then counts as claimed.
	_ = core.Inject(mod.node, &mod.OptionalDeps)

	return
}
