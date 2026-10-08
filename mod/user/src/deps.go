package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/crypto"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/nearby"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/scheduler"
	"github.com/astralp2p/astrald/mod/shell"
	"github.com/astralp2p/astrald/mod/tree"
)

type Deps struct {
	Auth      authmod.Module
	Crypto    crypto.Module
	Dir       dir.Module
	Objects   objectsmod.Module
	Nodes     nodesmod.Module
	Scheduler scheduler.Module
	Shell     shell.Module
	Nearby    nearby.Module
	Tree      tree.Module
}

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	// bind the config
	err = tree.BindPath(ctx, &mod.config, mod.Tree.Root(), "/mod/user/config", true)
	if err != nil {
		return err
	}

	mod.Auth.Add(authmod.Func[*nodes.RelayForAction](mod.AuthorizeRelayFor))
	mod.Auth.Add(authmod.Func[*auth.SeeObjectsAction](mod.AuthorizeSeeObjects))
	// why NodeLocal: membership is this node's record of its swarm, so a sibling
	// must not hand its device reads on through a contract.
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*auth.SeeObjectsAction](mod.AuthorizeSiblingSeeObjects)))
	mod.Auth.Add(authmod.Func[*auth.StoreObjectsAction](mod.AuthorizeStoreObjects))
	mod.Auth.Add(authmod.Func[*auth.AdminObjectsAction](mod.AuthorizeAdminObjects))
	mod.Auth.Add(authmod.Func[*auth.ServeObjectsAction](mod.AuthorizeServeObjects))
	mod.Auth.Add(authmod.Func[*services.ServiceDiscoveryAction](mod.AuthorizeServiceDiscovery))
	mod.Auth.Add(authmod.Func[*user.SeeSwarmAction](mod.AuthorizeSeeSwarm))
	mod.Auth.Add(authmod.Func[*user.AdminSwarmAction](mod.AuthorizeAdminSwarm))
	mod.Auth.Add(authmod.Func[*auth.AdminNetworkAction](mod.AuthorizeAdminNetwork))
	mod.Auth.Add(authmod.Func[*auth.AdminManageAppsAction](mod.AuthorizeAdminManageApps))
	mod.Auth.Add(authmod.Func[*auth.ConfigureNodeStateAction](mod.AuthorizeConfigureNodeState))
	mod.Auth.Add(authmod.Func[*auth.ServeAppsAction](mod.AuthorizeServeApps))
	mod.Auth.Add(authmod.Func[*auth.SeeNodeStateAction](mod.AuthorizeSeeNodeState))
	mod.Auth.Add(authmod.Func[*shell.ShellAction](mod.AuthorizeShell))

	// why: the network group belongs to main, so a read that misses this device reaches the siblings.
	err = mod.Objects.AddRepository(siblingsRepo, &SiblingRepository{mod: mod})
	if err != nil {
		return
	}

	err = mod.Objects.AddGroup(objects.RepoNetwork, siblingsRepo)
	if err != nil {
		return
	}

	// why: localuser as a name, to match localuser as a filter
	err = mod.Dir.AddResolver(mod)
	if err != nil {
		return
	}

	// add localswarm filter
	mod.Dir.SetFilter("localswarm", func(identity *astral.Identity) bool {
		if identity.IsZero() {
			return false
		}
		for _, swarm := range mod.LocalSwarm() {
			if identity.IsEqual(swarm) {
				return true
			}
		}
		return false
	})

	// add localuser filter
	mod.Dir.SetFilter("localuser", func(identity *astral.Identity) bool {
		if identity.IsZero() {
			return false
		}
		return identity.IsEqual(mod.Identity())
	})

	return
}
