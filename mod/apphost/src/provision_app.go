package apphost

import (
	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

// ProvisionApp mints an App identity holding a node→app contract of
// ServeApps, UseBlueprints and contractPermits; see apphost.Module.
// sys.register_manager provisions the manager with it.
//
// why the two first: the manager is served from this node and syncs its
// blueprints, as every registration asks (fileRegisterPermits).
func (mod *Module) ProvisionApp(ctx *astral.Context, contractPermits []*auth.Permit) (*apphostapi.AccessToken, error) {
	return mod.provisionApp(ctx, append(hostingPermits(), contractPermits...))
}

// hostingPermits are the permits every app asks for: to be served from this
// node and to sync its blueprints.
func hostingPermits() []*auth.Permit {
	return []*auth.Permit{
		{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())},
		{Action: astral.String8(auth.UseBlueprintsAction{}.ObjectType())},
	}
}

// RequestContract files a ContractRequest asking permits for app; see
// apphost.Module.
func (mod *Module) RequestContract(ctx *astral.Context, app *astral.Identity, permits []*auth.Permit) (*astral.ObjectID, error) {
	if mod.contractRequests == nil {
		return nil, apphostmod.ErrContractRequestsOff
	}
	return mod.contractRequests.File(ctx, app, permits)
}
