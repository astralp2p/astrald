package user

import (
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/apphost"
	"github.com/astralp2p/astrald/mod/auth"
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
	Apphost   apphost.Module
	Auth      auth.Module
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

	mod.Auth.Add(auth.Func[*nodes.RelayForAction](mod.AuthorizeRelayFor))
	mod.Auth.Add(auth.Func[*objects.ReadObjectAction](mod.AuthorizeReadObject))
	mod.Auth.Add(auth.Func[*user.ExpelAction](mod.AuthorizeExpel))
	mod.Auth.Add(auth.Func[*user.AdoptAction](mod.AuthorizeAdopt))
	mod.Auth.Add(auth.Func[*user.InfoAction](mod.AuthorizeInfo))

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
