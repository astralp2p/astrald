package auth

import (
	"github.com/astralp2p/astral-go/api/user"
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

// claimState is the part of the user module IsNodeClaim reads: whether the
// node has a user yet.
type claimState interface {
	Ready() <-chan struct{}
	Identity() *astral.Identity
}

// swarmReader is the part of the user module's SwarmView (mod/user) the walk
// reads. The walk follows each contract to its issuer, and the auth index
// holds only contracts with both signatures valid. At every level of a walk:
//   - root: after the veto, the walk allows the actor when it is the root key
//     or holds a PrivilegedMembership the root key signed, and is not expelled,
//     at any hop and for any action except user.ExpelAction and
//     auth.SudoAction (swarmRoot);
//   - user powers: a contract whose issuer is not the root key is not followed
//     for user.ExpelAction, user.AdminSwarmAction, a nodes.RelayForAction for
//     the root key or an auth.SudoAction as the root key
//     (userPowerFromNonRoot). A Privileged member uses AdminSwarm and relays as
//     the user itself, as the actor, and passes neither on.
//
// The walk reads no SwarmState entry: a contract's RequestID adds no authority.
// The root key is the user this node is claimed by, or this node while it is
// unclaimed (rootKey).
// Spec: .ai/system/protocols/auth/README.md#authority-in-a-swarm
type swarmReader interface {
	Expelled(id *astral.Identity) bool
	Membership(id *astral.Identity) (*user.Membership, bool)
}

// userModule is the part of the user module auth reads.
//
// why declared here and not mod/user's Module: auth depends on no more of the
// user module than it reads. core.Inject matches the "user" module by field
// name and assignability, so the narrow interface injects as well.
type userModule interface {
	claimState
	swarmReader
}

// why: LoadDependencies ignores the OptionalDeps injection error, so a user
// module that stopped satisfying userModule would leave User nil and the walk
// would read no swarm; this fails the build instead.
var _ userModule = (usermod.Module)(nil)

// OptionalDeps holds the user module. Without it the node counts as claimed,
// and the walk reads no swarm.
type OptionalDeps struct {
	User userModule
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
