package apphost

import (
	"fmt"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	sysapi "github.com/astralp2p/astral-go/api/sys"
	"github.com/astralp2p/astral-go/astral"
)

// ProvisionApp mints an App identity without asking the app register policy;
// see apphost.Module. sys.register_manager provisions the manager with it.
func (mod *Module) ProvisionApp(ctx *astral.Context, grantPermits, contractPermits []*auth.Permit) (*apphostapi.AccessToken, error) {
	return mod.provisionApp(ctx, &apphostapi.AppRegisterDecision{
		Allow:           true,
		GrantPermits:    registerGrantRequest(grantPermits),
		ContractPermits: contractPermits,
	})
}

// SetPolicyDelegate stores d as the delegate of an apphost policy, or clears
// it when d is nil; see apphost.Module.
func (mod *Module) SetPolicyDelegate(ctx *astral.Context, name string, d *policy.Delegate) error {
	switch name {
	case sysapi.PolicyAppRegister:
		return writePolicyDelegate(ctx, d, &mod.policy.AppRegister)
	case sysapi.PolicyGrantRequest:
		return writePolicyDelegate(ctx, d, &mod.policy.GrantRequest)
	}
	return fmt.Errorf("not an apphost policy: %v", name)
}
