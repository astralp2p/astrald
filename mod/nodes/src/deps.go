package nodes

import (
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/crypto"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/events"
	"github.com/astralp2p/astrald/mod/exonet"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/scheduler"
	"github.com/astralp2p/astrald/mod/user"
)

type Deps struct {
	Auth      auth.Module
	Crypto    crypto.Module
	Dir       dir.Module
	User      user.Module
	Exonet    exonet.Module
	Objects   objects.Module
	Scheduler scheduler.Module
	Events    events.Module
}

// LoadDependencies injects deps and registers the "linked" directory filter and
// the relay-for authorizer.
func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	mod.Dir.SetFilter("linked", func(identity *astral.Identity) bool {
		return mod.IsLinked(identity)
	})

	mod.Auth.Add(auth.Func[*nodes.RelayForAction](mod.AuthorizeRelayFor))

	return err
}
