package objects

import (
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// LoadDependencies injects core deps, then scans every other loaded module and
// auto-registers it under whichever objects extension interfaces it implements
// (Describer, Searcher, Finder, etc).
func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	if cnode, ok := mod.node.(*core.Node); ok {
		for _, m := range cnode.Modules().Loaded() {
			if m == mod {
				continue
			}

			if d, ok := m.(objects.Describer); ok {
				mod.AddDescriber(d)
			}

			if d, ok := m.(objects.Searcher); ok {
				mod.AddSearcher(d)
			}

			if d, ok := m.(objects.SearchPreprocessor); ok {
				mod.AddSearchPreprocessor(d)
			}

			if d, ok := m.(objects.Finder); ok {
				mod.AddFinder(d)
			}

			if h, ok := m.(objectsmod.Holder); ok {
				mod.AddHolder(h)
			}

			if r, ok := m.(objectsmod.Receiver); ok {
				mod.AddReceiver(r)
			}
		}
	}

	return
}
