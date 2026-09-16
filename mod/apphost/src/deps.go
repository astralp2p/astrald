package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/coldcard"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/shell"
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
	//
	// why: every one is NodeLocal. Each answers from this node's grant rows, and a
	// grant travels nowhere, so a contract chain must not reach one — otherwise a
	// grant holder extends the grant a hop by issuing a contract for the action.
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*auth.ServeObjectsAction](mod.AuthorizeServeObjects)))
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*auth.SeeObjectsAction](mod.AuthorizeSeeObjects)))
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*auth.StoreObjectsAction](mod.AuthorizeStoreObjects)))
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*user.SeeSwarmAction](mod.AuthorizeSeeSwarm)))
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*user.AdminSwarmAction](mod.AuthorizeAdminSwarm)))
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*auth.AdminNetworkAction](mod.AuthorizeAdminNetwork)))
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*auth.AdminManageAppsAction](mod.AuthorizeAdminManageApps)))
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*auth.ConfigureNodeStateAction](mod.AuthorizeConfigureNodeState)))
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*auth.ServeAppsAction](mod.AuthorizeServeApps)))
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*auth.SeeNodeStateAction](mod.AuthorizeSeeNodeState)))
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*coldcard.ScanAction](mod.AuthorizeColdcardScan)))
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*shell.ShellAction](mod.AuthorizeShell)))

	return
}
