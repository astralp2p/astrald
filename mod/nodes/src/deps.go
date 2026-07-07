package nodes

import (
	"github.com/cryptopunkscc/astral-go/api/nodes"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/mod/crypto"
	"github.com/cryptopunkscc/astrald/mod/dir"
	"github.com/cryptopunkscc/astrald/mod/events"
	"github.com/cryptopunkscc/astrald/mod/exonet"
	"github.com/cryptopunkscc/astrald/mod/objects"
	"github.com/cryptopunkscc/astrald/mod/scheduler"
	"github.com/cryptopunkscc/astrald/mod/user"
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
