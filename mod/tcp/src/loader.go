package tcp

import (
	"fmt"
	tcpmod "github.com/cryptopunkscc/astrald/mod/tcp"
	"strings"

	"github.com/cryptopunkscc/astral-go/api/tcp"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/core/assets"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, l *log.Logger) (core.Module, error) {
	mod := &Module{
		node:   node,
		log:    l,
		config: defaultConfig,
	}

	_ = assets.LoadYAML(tcpmod.ModuleName, &mod.config)

	mod.router.AddStructPrefix(mod, "Op")

	for _, addr := range mod.config.Endpoints {
		addr, _ = strings.CutPrefix(addr, fmt.Sprintf("%s:", tcpmod.ModuleName))

		endpoint, err := tcp.ParseEndpoint(addr)
		if err != nil {
			mod.log.Errorv(0, "tcp module/Load invalid endpoint: %v", addr)
		}

		mod.configEndpoints = append(mod.configEndpoints, endpoint)
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(tcpmod.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
