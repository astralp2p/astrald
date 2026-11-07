package user

import (
	"fmt"

	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/sys"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// policyPath is where the user module persists its delegates.
const policyPath = "/mod/user/policy"

// policyConfig is bound to the tree at policyPath. An unset SwarmJoin or
// SwarmInvite selects the config value; see GetSwarmJoinPolicy. An unset
// ContractAccept selects the bundled policy: every valid contract accepted.
type policyConfig struct {
	SwarmJoin      tree.Value[*policy.Delegate]
	SwarmInvite    tree.Value[*policy.Delegate]
	ContractAccept tree.Value[*policy.Delegate]
}

// GetSwarmJoinPolicy returns the policy user.request_membership applies: the
// delegate the tree names; else the policies.swarm_join config value, whose
// code default is reject_all.
//
// why derived on each call: the tree value is the only state, so a setter
// writes it and nothing else, and a reload selects the same policy.
// why a config delegate that does not resolve declines: an unknown delegate
// has decided nothing.
func (mod *Module) GetSwarmJoinPolicy() usermod.SwarmJoinPolicy {
	if d := mod.policy.SwarmJoin.Get(); !d.IsZero() {
		return mod.SwarmJoinPolicyDelegated(d)
	}

	setting := mod.file.Policies.SwarmJoin
	switch {
	case setting.Delegate != nil:
		d, err := setting.ResolveDelegate(mod.Dir.ResolveIdentity)
		if err != nil {
			return func(*astral.Context, *user.SwarmJoinRequest) (*user.SwarmJoinDecision, error) { return nil, err }
		}
		return mod.SwarmJoinPolicyDelegated(d)
	case setting.Accepts():
		return usermod.AcceptAllJoins
	}
	return usermod.RejectAllJoins
}

// GetSwarmInvitePolicy returns the policy user.accept_membership applies: the
// delegate the tree names; else the policies.swarm_invite config value, whose
// code default is reject_all.
func (mod *Module) GetSwarmInvitePolicy() usermod.SwarmInvitePolicy {
	if d := mod.policy.SwarmInvite.Get(); !d.IsZero() {
		return mod.SwarmInvitePolicyDelegated(d)
	}

	setting := mod.file.Policies.SwarmInvite
	switch {
	case setting.Delegate != nil:
		d, err := setting.ResolveDelegate(mod.Dir.ResolveIdentity)
		if err != nil {
			return func(*astral.Context, *user.SwarmInviteRequest) (*user.SwarmInviteDecision, error) { return nil, err }
		}
		return mod.SwarmInvitePolicyDelegated(d)
	case setting.Accepts():
		return usermod.AcceptAllInvites
	}
	return usermod.RejectAllInvites
}

// SetPolicyDelegate stores d as the delegate of the named policy, or clears it
// when d is nil.
func (mod *Module) SetPolicyDelegate(ctx *astral.Context, name string, d *policy.Delegate) error {
	var v *tree.Value[*policy.Delegate]
	switch name {
	case sys.PolicySwarmJoin:
		v = &mod.policy.SwarmJoin
	case sys.PolicySwarmInvite:
		v = &mod.policy.SwarmInvite
	case sys.PolicyContractAccept:
		v = &mod.policy.ContractAccept
	default:
		return fmt.Errorf("unknown user policy: %v", name)
	}
	if d == nil {
		return v.Clear(ctx)
	}
	return v.Set(ctx, d)
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
