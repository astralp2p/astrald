package apphost

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/core"
)

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	if err = core.Inject(mod.node, &mod.Deps); err != nil {
		return
	}

	// optional — apphost can run without user module
	core.Inject(mod.node, &mod.OptionalDeps)

	// why: an app's permits come from contracts its host signs when an
	// Approval in SwarmState answers the app's ContractRequest; without the
	// user module there is no SwarmState to file one in.
	if mod.User != nil {
		mod.contractRequests = newContractRequests(mod.node.Identity(), mod.User, mod.User, mod.Crypto, mod.Auth)
		if n := mod.config.ContractRequestsPerNode; n > 0 {
			mod.contractRequests.limiter = newRequestLimiter(ContractRequestWindow, ContractRequestsPerApp, n)
		}
	}

	op, err := routing.NewOp(mod.requestContract)
	if err != nil {
		return err
	}
	return mod.router.AddOp("request_contract", op)
}
