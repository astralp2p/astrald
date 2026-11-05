package nearby

import (
	"fmt"
	"strings"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/crypto"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/ether"
	"github.com/astralp2p/astrald/mod/events"
	"github.com/astralp2p/astrald/mod/nearby"
	"github.com/astralp2p/astrald/mod/nodes"
	treemod "github.com/astralp2p/astrald/mod/tree"
	"github.com/astralp2p/astrald/mod/user"
)

var (
	modePath         = fmt.Sprintf("/mod/%s/mode", nearby.ModuleName)
	silentListenPath = fmt.Sprintf("/mod/%s/silent_listen", nearby.ModuleName)
)

type Deps struct {
	Auth   auth.Module
	Crypto crypto.Module
	Dir    dir.Module
	Ether  ether.Module
	Events events.Module
	Nodes  nodes.Module
	Tree   treemod.Module
	User   user.Module
}

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	// why before Run: ether delivers scans once it runs, and a scan answered
	// before the configured mode is stored reads as Visible.
	if err = mod.bindSettings(ctx, mod.Tree.Root()); err != nil {
		return
	}

	mod.Dir.AddResolver(mod)
	mod.Nodes.AddResolver(mod)

	if cnode, ok := mod.node.(*core.Node); ok {
		mod.addPlugins(cnode.Modules().Loaded())
	}

	return nil
}

// bindSettings binds the mode and silent_listen to the tree under root and writes the
// values nearby.yaml states.
func (mod *Module) bindSettings(ctx *astral.Context, root tree.Node) error {
	if err := mod.mode.BindPath(ctx, root, modePath, true); err != nil {
		return err
	}
	if err := mod.silentListen.BindPath(ctx, root, silentListenPath, true); err != nil {
		return err
	}
	return mod.syncConfig(ctx)
}

// syncConfig writes each setting nearby.yaml states into the tree; a setting the file
// leaves out keeps its stored value.
func (mod *Module) syncConfig(ctx *astral.Context) error {
	if mod.config.Mode != nil {
		if err := mod.SetMode(ctx, *mod.config.Mode); err != nil {
			return err
		}
	}
	if mod.config.SilentListen != nil {
		v := astral.Bool(*mod.config.SilentListen)
		if err := mod.silentListen.Set(ctx, &v); err != nil {
			return err
		}
	}
	return nil
}

// addPlugins registers every loaded module other than nearby that composes or
// recognizes statuses.
func (mod *Module) addPlugins(modules []core.Module) {
	var composers, recognizers []any
	for _, m := range modules {
		if m == mod {
			continue
		}
		if c, ok := m.(nearby.Composer); ok {
			mod.AddStatusComposer(c)
			composers = append(composers, c)
		}
		if r, ok := m.(nearby.Recognizer); ok {
			mod.recognizers.Add(r)
			recognizers = append(recognizers, r)
		}
	}

	if len(composers) > 0 {
		mod.log.Logv(2, "composers: %v"+strings.Repeat(", %v", len(composers)-1), composers...)
	}
	if len(recognizers) > 0 {
		mod.log.Logv(2, "recognizers: %v"+strings.Repeat(", %v", len(recognizers)-1), recognizers...)
	}
}
