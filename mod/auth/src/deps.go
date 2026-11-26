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

// swarmReader is the part of the user module's SwarmState and SwarmView
// (mod/user) the walk reads. At every level of a walk:
//   - root: after the veto, the walk allows the actor when it is the root key
//     or holds a PrivilegedMembership the root key signed, and is not expelled,
//     at any hop and for any action except user.ExpelAction and
//     auth.SudoAction (swarmRoot);
//   - Approval link: a contract with an ApprovalID holds only when that
//     Approval is stored, its request is approved, the request was filed (a
//     ContractRequest) or carried in (a JoinRequest) by the contract's issuer,
//     names its subject and covers its permits (a JoinRequest: at the tier the
//     contract grants), the approver is the root key or its tier may answer
//     that request (user.MayAnswer), and neither the issuer, the subject nor
//     the approver is expelled or vetoed. The walk then continues as the
//     approver instead of the issuer. A contract with no ApprovalID is followed
//     to its issuer as before.
//
// The root key is the user this node is claimed by, or this node while it is
// unclaimed (rootKey).
type swarmReader interface {
	SwarmEntry(id *astral.ObjectID) (astral.Object, error)
	RequestStatus(req *astral.ObjectID) user.RequestStatus
	Expelled(id *astral.Identity) bool
	Membership(id *astral.Identity) (*user.Membership, bool)
	MayAnswer(signer *astral.Identity, req *astral.ObjectID) bool
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
