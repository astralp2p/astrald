package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
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
	// action type. Each names a shim in authorizers.go over one generic lookup, so
	// the list grows by a line and never by a decision. A wildcard authorizer
	// would replace the list rather than each entry.
	mod.Auth.Add(authmod.Func[*auth.ServeObjectsAction](mod.AuthorizeServeObjects))
	mod.Auth.Add(authmod.Func[*user.SeeSwarmAction](mod.AuthorizeSeeSwarm))
	mod.Auth.Add(authmod.Func[*user.AdminSwarmAction](mod.AuthorizeAdminSwarm))

	return
}
