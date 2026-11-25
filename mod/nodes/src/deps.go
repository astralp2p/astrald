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
	ipmod "github.com/astralp2p/astrald/mod/ip"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/scheduler"
	"github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/mod/vfs"
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
	VFS       vfs.Module
}

// LoadDependencies injects deps and registers the "linked" directory filter and
// the relay-for authorizer.
func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	// why optional: a node without the ip module (no tcp or kcp) still links,
	// and core.Inject fails on a missing module.
	mod.subnets, _ = core.Load[subnetLister](mod.node, ipmod.ModuleName)
	mod.fingerprints, _ = core.Load[fingerprinter](mod.node, ipmod.ModuleName)

	mod.Dir.SetFilter("linked", func(identity *astral.Identity) bool {
		return mod.IsLinked(identity)
	})

	mod.Auth.Add(auth.Func[*nodes.RelayForAction](mod.AuthorizeRelayFor))

	return err
}
