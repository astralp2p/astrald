package nearby

import (
	"fmt"
	"strings"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/ether"
	"github.com/astralp2p/astrald/mod/exonet"
	"github.com/astralp2p/astrald/mod/nearby"
	"github.com/astralp2p/astrald/mod/nodes"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/shell"
	"github.com/astralp2p/astrald/mod/tcp"
	"github.com/astralp2p/astrald/mod/tree"
	"github.com/astralp2p/astrald/mod/user"
)

type Deps struct {
	Auth    auth.Module
	Dir     dir.Module
	Ether   ether.Module
	Exonet  exonet.Module
	Nodes   nodes.Module
	Objects objects.Module
	User    user.Module
	Shell   shell.Module
	TCP     tcp.Module
	Tree    tree.Module
}

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	var modePath = fmt.Sprintf("/mod/%s/mode", nearby.ModuleName)
	err = mod.mode.BindPath(ctx, mod.Tree.Root(), modePath, true)
	if err != nil {
		return
	}

	mod.Dir.AddResolver(mod)
	mod.Nodes.AddResolver(mod)

	if cnode, ok := mod.node.(*core.Node); ok {
		var composers []any
		for _, m := range cnode.Modules().Loaded() {
			if m == mod {
				continue
			}
			if a, ok := m.(nearby.Composer); ok {
				mod.AddStatusComposer(a)
				composers = append(composers, a)
			}

		}

		if mod.composers.Count() > 0 {
			mod.log.Logv(2, "composers: %v"+strings.Repeat(", %v", len(composers)-1), composers...)
		}
	}

	return nil
}
