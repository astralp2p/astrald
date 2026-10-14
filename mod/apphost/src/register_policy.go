package apphost

import (
	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/astral"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

// GetAppRegisterPolicy returns the policy apphost.register applies: the
// delegated policy when a delegate is set, accept-all otherwise.
func (mod *Module) GetAppRegisterPolicy() apphostmod.AppRegisterPolicy {
	mod.policyMu.RLock()
	defer mod.policyMu.RUnlock()

	if mod.appRegisterPolicy == nil {
		return mod.AppRegisterAcceptAll
	}
	return mod.appRegisterPolicy
}

// SetAppRegisterPolicy persists the delegate, then selects the policy it names.
// A zero delegate restores accept-all. A failed save leaves the policy as it
// was.
func (mod *Module) SetAppRegisterPolicy(ctx *astral.Context, d *policy.PolicyDelegate) (err error) {
	mod.policyMu.Lock()
	defer mod.policyMu.Unlock()

	if d.IsZero() {
		err = mod.policy.AppRegister.Clear(ctx)
	} else {
		err = mod.policy.AppRegister.Set(ctx, d)
	}
	if err != nil {
		return err
	}

	mod.appRegisterPolicy = mod.selectAppRegisterPolicy(d)

	return nil
}

// selectAppRegisterPolicy returns the policy a delegate names.
func (mod *Module) selectAppRegisterPolicy(d *policy.PolicyDelegate) apphostmod.AppRegisterPolicy {
	if d.IsZero() {
		return mod.AppRegisterAcceptAll
	}
	return mod.AppRegisterPolicyDelegated(d)
}

var _ apphostmod.AppRegisterPolicy = (*Module)(nil).AppRegisterAcceptAll

// AppRegisterAcceptAll admits every registration and writes every permit put in
// front of it onto the rail it was asked for, whether the caller's origin
// entitled it or the app simply asked. It assigns no authorizers. It is the
// permissive default its name claims to be: a node that cares which apps hold
// what delegates the decision.
func (mod *Module) AppRegisterAcceptAll(_ *astral.Context, req *apphost.AppRegisterRequest) (*apphost.AppRegisterDecision, error) {
	mod.log.Info("accepting registration from origin %v with %v grant permits and %v contract permits",
		req.Origin, len(req.GrantPermits), len(req.ContractPermits))

	return &apphost.AppRegisterDecision{
		Allow:           true,
		GrantPermits:    req.GrantPermits,
		ContractPermits: req.ContractPermits,
	}, nil
}
