package user

import (
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// policyPath is where the user module persists its delegates.
const policyPath = "/mod/user/policy"

// policyConfig is bound to the tree at policyPath. An unset value selects the
// bundled policy: every join request, every invitation and every valid
// contract accepted.
type policyConfig struct {
	SwarmJoin      tree.Value[*policy.Delegate]
	SwarmInvite    tree.Value[*policy.Delegate]
	ContractAccept tree.Value[*policy.Delegate]
}

// GetSwarmJoinPolicy returns the policy user.request_membership applies: the
// delegate the tree names, or AcceptAllJoins when none is set.
//
// why derived on each call: the tree value is the only state, so a setter
// writes it and nothing else, and a reload selects the same policy.
func (mod *Module) GetSwarmJoinPolicy() usermod.SwarmJoinPolicy {
	d := mod.policy.SwarmJoin.Get()
	if d.IsZero() {
		return usermod.AcceptAllJoins
	}
	return mod.SwarmJoinPolicyDelegated(d)
}

// GetSwarmInvitePolicy returns the policy user.accept_membership applies: the
// delegate the tree names, or AcceptAllInvites when none is set.
func (mod *Module) GetSwarmInvitePolicy() usermod.SwarmInvitePolicy {
	d := mod.policy.SwarmInvite.Get()
	if d.IsZero() {
		return usermod.AcceptAllInvites
	}
	return mod.SwarmInvitePolicyDelegated(d)
}

// GetContractAcceptPolicy returns the policy user.accept_contract applies: the
// delegate the tree names, or AcceptAllContracts when none is set.
func (mod *Module) GetContractAcceptPolicy() usermod.ContractAcceptPolicy {
	d := mod.policy.ContractAccept.Get()
	if d.IsZero() {
		return usermod.AcceptAllContracts
	}
	return mod.ContractAcceptPolicyDelegated(d)
}

// SwarmJoinPolicyDelegated returns the join policy that asks d. The policy
// holds d, so a later change of delegate does not move a question already
// asked. A failure declines: a delegate that cannot be reached has decided
// nothing.
func (mod *Module) SwarmJoinPolicyDelegated(d *policy.Delegate) usermod.SwarmJoinPolicy {
	return func(ctx *astral.Context, req *user.SwarmJoinRequest) (*user.SwarmJoinDecision, error) {
		obj, err := policy.Ask(ctx, mod.node, d, req)
		if err != nil {
			return nil, err
		}
		// why: any other object is an answer this code cannot read, and an
		// unreadable answer must not read as permission.
		dec, ok := obj.(*user.SwarmJoinDecision)
		if !ok {
			return nil, astral.NewErrUnexpectedObject(obj)
		}
		return dec, nil
	}
}

// SwarmInvitePolicyDelegated returns the invite policy that asks d. The policy
// holds d, so a later change of delegate does not move a question already
// asked. A failure declines.
func (mod *Module) SwarmInvitePolicyDelegated(d *policy.Delegate) usermod.SwarmInvitePolicy {
	return func(ctx *astral.Context, req *user.SwarmInviteRequest) (*user.SwarmInviteDecision, error) {
		obj, err := policy.Ask(ctx, mod.node, d, req)
		if err != nil {
			return nil, err
		}
		dec, ok := obj.(*user.SwarmInviteDecision)
		if !ok {
			return nil, astral.NewErrUnexpectedObject(obj)
		}
		return dec, nil
	}
}

// ContractAcceptPolicyDelegated returns the contract accept policy that asks
// d. The policy holds d, so a later change of delegate does not move a
// question already asked. A failure declines.
func (mod *Module) ContractAcceptPolicyDelegated(d *policy.Delegate) usermod.ContractAcceptPolicy {
	return func(ctx *astral.Context, req *user.ContractAcceptRequest) (*user.ContractAcceptDecision, error) {
		obj, err := policy.Ask(ctx, mod.node, d, req)
		if err != nil {
			return nil, err
		}
		dec, ok := obj.(*user.ContractAcceptDecision)
		if !ok {
			return nil, astral.NewErrUnexpectedObject(obj)
		}
		return dec, nil
	}
}
