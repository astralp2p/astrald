package tcp

import (
	"fmt"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/core/assets"
	"github.com/cryptopunkscc/astrald/mod/tcp"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, l *log.Logger) (core.Module, error) {
	mod := &Module{
		node:   node,
		log:    l,
		config: defaultConfig,
	}

	_ = assets.LoadYAML(tcp.ModuleName, &mod.config)

	mod.router.AddStructPrefix(mod, "Op")

	for _, addr := range mod.config.Endpoints {
		addr, _ = strings.CutPrefix(addr, fmt.Sprintf("%s:", tcp.ModuleName))

		endpoint, err := tcp.ParseEndpoint(addr)
		if err != nil {
			mod.log.Errorv(0, "tcp module/Load invalid endpoint: %v", addr)
		}

		mod.configEndpoints = append(mod.configEndpoints, endpoint)
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(tcp.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
