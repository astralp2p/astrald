package shell

import (
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/mod/dir"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

type Deps struct {
	Auth    auth.Module
	Dir     dir.Module
	Objects objects.Module
}

type HasRouter interface {
	Router() astral.Router
}

func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	if cnode, ok := mod.node.(*core.Node); ok {
		var added []any
		for _, m := range cnode.Modules().Loaded() {
			if m == mod {
				continue
			}

			if s, ok := m.(HasRouter); ok {
				mod.scopes.Add(astral.Stringify(s), s.Router())
				added = append(added, m)
			}
		}
		if len(added) > 0 {
			mod.log.Logv(2, "shell scopes: %v"+strings.Repeat(", %v", len(added)-1), added...)
		}
	}

	return
}
