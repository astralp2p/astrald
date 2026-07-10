package ip

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/ip"
)

func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return err
	}

	if cnode, ok := mod.node.(*core.Node); ok {
		for _, m := range cnode.Modules().Loaded() {
			if m == mod {
				continue
			}

			if d, ok := m.(ip.PublicIPCandidateProvider); ok {
				mod.AddPublicIPCandidateProvider(d)
			}

		}
	}

	return nil
}
