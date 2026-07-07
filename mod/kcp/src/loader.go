package kcp

import (
	"fmt"
	kcpmod "github.com/cryptopunkscc/astrald/mod/kcp"
	"strings"

	"github.com/cryptopunkscc/astral-go/api/kcp"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/core/assets"
)

type Loader struct{}

// Load constructs the Module, merges YAML config over defaults, and registers
// the "Op"-prefixed operation router entries before returning.
func (Loader) Load(node astral.Node, assets assets.Assets, l *log.Logger) (core.Module, error) {
	mod := &Module{
		node:   node,
		log:    l,
		config: defaultConfig,
	}

	_ = assets.LoadYAML(kcpmod.ModuleName, &mod.config)

	mod.router.AddStructPrefix(mod, "Op")

	for _, addr := range mod.config.Endpoints {
		addr, _ = strings.CutPrefix(addr, fmt.Sprintf("%s:", kcpmod.ModuleName))

		endpoint, err := kcp.ParseEndpoint(addr)
		if err != nil {
			mod.log.Errorv(0, "kcp module/Load invalid endpoint: %v", addr)
		}

		mod.configEndpoints = append(mod.configEndpoints, endpoint)
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(kcpmod.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
