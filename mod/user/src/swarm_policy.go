package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// GetSwarmJoinRequestPolicy returns the join policy: the delegated policy when
// a delegate is set, accept-all otherwise.
func (mod *Module) GetSwarmJoinRequestPolicy() usermod.SwarmJoinRequestPolicy {
	mod.policyMu.RLock()
	defer mod.policyMu.RUnlock()

	if mod.joinPolicy == nil {
		return mod.SwarmJoinRequestAcceptAll
	}
	return mod.joinPolicy
}

// GetSwarmInvitePolicy returns the invite policy: the delegated policy when a
// delegate is set, accept-all otherwise.
func (mod *Module) GetSwarmInvitePolicy() usermod.SwarmInvitePolicy {
	mod.policyMu.RLock()
	defer mod.policyMu.RUnlock()

	if mod.invitePolicy == nil {
		return mod.SwarmInviteAcceptAll
	}
	return mod.invitePolicy
}

// SetSwarmJoinRequestPolicy persists the delegate, then selects the policy it
// names. A zero delegate restores accept-all. A failed save leaves the policy
// as it was.
func (mod *Module) SetSwarmJoinRequestPolicy(ctx *astral.Context, d *policy.PolicyDelegate) error {
	mod.policyMu.Lock()
	defer mod.policyMu.Unlock()

	if err := saveDelegate(ctx, &mod.policy.SwarmJoin, d); err != nil {
		return err
	}
	mod.joinPolicy = mod.selectJoinPolicy(d)

	return nil
}

// SetSwarmInvitePolicy persists the delegate, then selects the policy it names.
// A zero delegate restores accept-all. A failed save leaves the policy as it
// was.
func (mod *Module) SetSwarmInvitePolicy(ctx *astral.Context, d *policy.PolicyDelegate) error {
	mod.policyMu.Lock()
	defer mod.policyMu.Unlock()

	if err := saveDelegate(ctx, &mod.policy.SwarmInvite, d); err != nil {
		return err
	}
	mod.invitePolicy = mod.selectInvitePolicy(d)

	return nil
}

func (mod *Module) selectJoinPolicy(d *policy.PolicyDelegate) usermod.SwarmJoinRequestPolicy {
	if d.IsZero() {
		return mod.SwarmJoinRequestAcceptAll
	}
	return mod.SwarmJoinRequestPolicyDelegated(d)
}

func (mod *Module) selectInvitePolicy(d *policy.PolicyDelegate) usermod.SwarmInvitePolicy {
	if d.IsZero() {
		return mod.SwarmInviteAcceptAll
	}
	return mod.SwarmInvitePolicyDelegated(d)
}

var _ usermod.SwarmJoinRequestPolicy = (*Module)(nil).SwarmJoinRequestAcceptAll

func (mod *Module) SwarmJoinRequestAcceptAll(_ *astral.Context, requester *astral.Identity) bool {
	mod.log.Info("Accepting %v join request into swarm", requester)
	return true
}

var _ usermod.SwarmInvitePolicy = (*Module)(nil).SwarmInviteAcceptAll

func (mod *Module) SwarmInviteAcceptAll(_ *astral.Context, invitee *astral.Identity, contract *auth.Contract) bool {
	mod.log.Info("Accepting invitation from %v for %v join swarm till %v", invitee, contract.Subject, contract.ExpiresAt)
	return true
}
