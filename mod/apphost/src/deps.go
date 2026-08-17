package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	if err = core.Inject(mod.node, &mod.Deps); err != nil {
		return
	}

	// optional — apphost can run without user module
	core.Inject(mod.node, &mod.OptionalDeps)

	// why: one line per grantable action, because Func dispatches on the concrete
	// action type. authorizeGrant itself is generic — see grants.go — so each
	// entry is an adapter, not a policy. A wildcard authorizer would replace this
	// list rather than each entry.
	mod.Auth.Add(authmod.Func[*auth.ServeObjectsAction](
		func(ctx *astral.Context, a *auth.ServeObjectsAction) bool {
			return mod.authorizeGrant(ctx, a)
		},
	))

	return
}
