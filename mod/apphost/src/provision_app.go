package apphost

import (
	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

// ProvisionApp mints an App identity holding a node→app contract of
// contractPermits; see apphost.Module. sys.register_manager provisions the
// manager with it.
func (mod *Module) ProvisionApp(ctx *astral.Context, contractPermits []*auth.Permit) (*apphostapi.AccessToken, error) {
	return mod.provisionApp(ctx, contractPermits)
}

// RequestContract files a ContractRequest asking permits for app; see
// apphost.Module.
func (mod *Module) RequestContract(ctx *astral.Context, app *astral.Identity, permits []*auth.Permit) (*astral.ObjectID, error) {
	if mod.contractRequests == nil {
		return nil, apphostmod.ErrContractRequestsOff
	}
	return mod.contractRequests.File(ctx, app, permits)
}
