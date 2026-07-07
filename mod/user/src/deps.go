package user

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/mod/apphost"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/mod/crypto"
	"github.com/cryptopunkscc/astrald/mod/dir"
	"github.com/cryptopunkscc/astrald/mod/nearby"
	"github.com/cryptopunkscc/astrald/mod/nodes"
	"github.com/cryptopunkscc/astrald/mod/objects"
	"github.com/cryptopunkscc/astrald/mod/scheduler"
	"github.com/cryptopunkscc/astrald/mod/shell"
	"github.com/cryptopunkscc/astrald/mod/tree"
	"github.com/cryptopunkscc/astrald/mod/user"
)

type Deps struct {
	Apphost   apphost.Module
	Auth      auth.Module
	Crypto    crypto.Module
	Dir       dir.Module
	Objects   objects.Module
	Nodes     nodes.Module
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
