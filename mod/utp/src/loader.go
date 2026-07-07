package utp

import (
	"fmt"
	utpmod "github.com/cryptopunkscc/astrald/mod/utp"
	"strings"

	"github.com/cryptopunkscc/astral-go/api/utp"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/core/assets"
)

type Loader struct{}

// Load constructs and configures the uTP module, stripping the "utp:" scheme
// prefix from any configured endpoint addresses before parsing them.
func (Loader) Load(node astral.Node, assets assets.Assets, l *log.Logger) (core.Module, error) {
	mod := &Module{
		node:   node,
		log:    l,
		config: defaultConfig,
	}

	_ = assets.LoadYAML(utpmod.ModuleName, &mod.config)

	for _, addr := range mod.config.Endpoints {
		addr, _ = strings.CutPrefix(addr, fmt.Sprintf("%s:", utpmod.ModuleName))

		endpoint, err := utp.ParseEndpoint(addr)
		if err != nil {
			mod.log.Errorv(0, "utp module/Load invalid endpoint: %v", addr)
		}

		mod.configEndpoints = append(mod.configEndpoints, endpoint)
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(utpmod.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
