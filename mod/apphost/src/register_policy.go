package apphost

import (
	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

// policyPath is where apphost persists its policy delegates.
const policyPath = "/mod/apphost/policy"

// policyConfig is bound to the tree at policyPath. An unset AppRegister
// selects apphostmod.AcceptAll; an unset GrantRequest selects
// apphostmod.RefuseAllGrants.
//
// why apart from Config: Config is read from apphost.yaml and copied from
// defaultConfig, and a tree.Value carries a mutex, so it cannot ride a copied
// struct.
type policyConfig struct {
	AppRegister  tree.Value[*policy.Delegate]
	GrantRequest tree.Value[*policy.Delegate]
}

// GetRegisterAppPolicy returns the policy apphost.register applies: the
// delegate the tree names, or AcceptAll when none is set.
//
// why derived on each call: the tree value is the only state, so a setter
// writes it and nothing else, and a reload selects the same policy.
func (mod *Module) GetRegisterAppPolicy() apphostmod.RegisterAppPolicy {
	d := mod.policy.AppRegister.Get()
	if d.IsZero() {
		return apphostmod.AcceptAll
	}
	return mod.RegisterAppPolicyDelegated(d)
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
// delegate the tree names, or RefuseAllGrants when none is set.
func (mod *Module) GetGrantRequestPolicy() apphostmod.GrantRequestPolicy {
	d := mod.policy.GrantRequest.Get()
	if d.IsZero() {
		return apphostmod.RefuseAllGrants
	}
	return mod.GrantRequestPolicyDelegated(d)
}

// GrantRequestPolicyDelegated returns the policy that asks d. The policy holds
// d, so a later change of delegate does not move a question already asked.
func (mod *Module) GrantRequestPolicyDelegated(d *policy.Delegate) apphostmod.GrantRequestPolicy {
	return func(ctx *astral.Context, req *apphost.GrantRequest) (*apphost.GrantDecision, error) {
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
