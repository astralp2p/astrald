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
//
// why the grant permits stay a separate list: writeAppAuthority signs them
// with Delegation zero, as for apphost.register, so the manager cannot pass on
// what it asked to hold on this node only.
func (mod *Module) ProvisionApp(ctx *astral.Context, grantPermits, contractPermits []*auth.Permit) (*apphostapi.AccessToken, error) {
	return mod.provisionApp(ctx, &apphostapi.AppRegisterDecision{
		Allow:           true,
		GrantPermits:    withHostingPermits(grantPermits),
		ContractPermits: contractPermits,
	})
}

// withHostingPermits returns unconstrained ServeApps and UseBlueprints
// permits, followed by the permits the app asked for, as sent, constraints
// included.
//
// why: a registered app is served from this node and syncs the blueprints of
// its types (apps.WithBlueprintSync), so every registration asks for hosting
// and for blueprints.
// note: the register policy decides whether they are written, as it decides
// for every other permit in the request.
func withHostingPermits(asked []*auth.Permit) []*auth.Permit {
	serveApps := &auth.Permit{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())}
	useBlueprints := &auth.Permit{Action: astral.String8(auth.UseBlueprintsAction{}.ObjectType())}
	return append([]*auth.Permit{serveApps, useBlueprints}, asked...)
}

// SetPolicyDelegate stores d as the delegate of an apphost policy, or clears
// it when d is nil; see apphost.Module.
func (mod *Module) SetPolicyDelegate(ctx *astral.Context, name string, d *policy.Delegate) error {
	switch name {
	case sysapi.PolicyAppRegister:
		return writePolicyDelegate(ctx, d, &mod.policy.AppRegister)
	}
	return fmt.Errorf("not an apphost policy: %v", name)
}
