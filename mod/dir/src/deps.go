package dir

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
)

func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	return core.Inject(mod.node, &mod.Deps)
}
