package coldcard

import (
	"github.com/astralp2p/astral-go/api/coldcard"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return err
	}

	// optional — the node scans as itself on a node that holds no user
	core.Inject(mod.node, &mod.OptionalDeps)

	mod.Auth.Add(authmod.Func[*coldcard.ScanAction](mod.AuthorizeScanAction))

	return err
}
