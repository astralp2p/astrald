package apphost

import (
	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

// policyPath is where apphost persists its policy delegates and the grant
// request mode.
const policyPath = "/mod/apphost/policy"

// policyConfig is bound to the tree at policyPath. An unset AppRegister
// selects the config value; see GetRegisterAppPolicy. A set GrantRequest
// selects the delegate mode; otherwise an unset GrantRequestMode or accept_all
// selects apphostmod.AcceptAllGrants, and any other mode selects
// apphostmod.RefuseAllGrants.
//
// why apart from Config: Config is read from apphost.yaml and copied from
// defaultConfig, and a tree.Value carries a mutex, so it cannot ride a copied
// struct.
//
// why GrantRequestMode holds any object: tree.Value reads a stored object of
// another type as nil, so a mode bound as String8 would read a wrong-typed
// value as unset and accept.
type policyConfig struct {
	AppRegister      tree.Value[*policy.Delegate]
	GrantRequest     tree.Value[*policy.Delegate]
	GrantRequestMode tree.Value[astral.Object]
}

// GetRegisterAppPolicy returns the policy apphost.register applies: the
// delegate the tree names; else the policies.app_register config value, whose
// code default is reject_all.
//
// why derived on each call: the tree value is the only state, so a setter
// writes it and nothing else, and a reload selects the same policy.
// why a config delegate that does not resolve refuses: an unknown delegate
// has decided nothing.
func (mod *Module) GetRegisterAppPolicy() apphostmod.RegisterAppPolicy {
	if d := mod.policy.AppRegister.Get(); !d.IsZero() {
		return mod.RegisterAppPolicyDelegated(d)
	}

	setting := mod.config.Policies.AppRegister
	switch {
	case setting.Delegate != nil:
		d, err := setting.ResolveDelegate(mod.Dir.ResolveIdentity)
		if err != nil {
			return func(*astral.Context, *apphost.AppRegisterRequest) (*apphost.AppRegisterDecision, error) {
				return nil, err
			}
		}
		return mod.RegisterAppPolicyDelegated(d)
	case setting.Accepts():
		return apphostmod.AcceptAll
	}
	return apphostmod.RejectAll
}

// RegisterAppPolicyDelegated returns the policy that asks d. The policy holds
// d, so a later change of delegate does not move a question already asked.
//
// why a failure refuses and not accepts: once registration is delegated, a
// delegate that cannot be reached has decided nothing, and falling back would
// let a stopped delegate open the node to every app.
func (mod *Module) RegisterAppPolicyDelegated(d *policy.Delegate) apphostmod.RegisterAppPolicy {
	return func(ctx *astral.Context, req *apphost.AppRegisterRequest) (*apphost.AppRegisterDecision, error) {
		obj, err := policy.Ask(ctx, mod.node, d, req)
		if err != nil {
			return nil, err
		}
		// why: any other object is an answer this code cannot read, and an
		// unreadable answer must not read as permission.
		dec, ok := obj.(*apphost.AppRegisterDecision)
		if !ok {
			return nil, astral.NewErrUnexpectedObject(obj)
		}
		return dec, nil
	}
}

// GetGrantRequestPolicy returns the policy apphost.request_grant applies: the
// delegate the tree names; else AcceptAllGrants when the mode is unset or
// accept_all; else RefuseAllGrants.
//
// why the delegate first: a delegate stored before the mode existed keeps
// deciding after an upgrade.
// why any other mode refuses: a mode this code cannot read must not read as
// permission.
func (mod *Module) GetGrantRequestPolicy() apphostmod.GrantRequestPolicy {
	if d := mod.policy.GrantRequest.Get(); !d.IsZero() {
		return mod.GrantRequestPolicyDelegated(d)
	}
	switch m := mod.policy.GrantRequestMode.Get().(type) {
	case nil:
		return apphostmod.AcceptAllGrants
	case *astral.String8:
		if m.String() == apphostmod.GrantRequestAcceptAll {
			return apphostmod.AcceptAllGrants
		}
	}
	return apphostmod.RefuseAllGrants
}

// GrantRequestPolicyDelegated returns the policy that asks d. The policy holds
// d, so a later change of delegate does not move a question already asked.
func (mod *Module) GrantRequestPolicyDelegated(d *policy.Delegate) apphostmod.GrantRequestPolicy {
	return func(ctx *astral.Context, req *apphost.GrantPolicyRequest) (*apphost.GrantDecision, error) {
		obj, err := policy.Ask(ctx, mod.node, d, req)
		if err != nil {
			return nil, err
		}
		// why: any other object is an answer this code cannot read, and an
		// unreadable answer must not read as permission.
		dec, ok := obj.(*apphost.GrantDecision)
		if !ok {
			return nil, astral.NewErrUnexpectedObject(obj)
		}
		return dec, nil
	}
}
