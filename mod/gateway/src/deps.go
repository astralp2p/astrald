package gateway

import (
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/core"
)

func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	mod.Exonet.SetDialer("gw", mod)
	mod.Exonet.SetUnpacker("gw", mod)
	mod.Exonet.SetParser("gw", mod)
	mod.router.AddStructPrefix(mod, "Op")
	mod.Services.AddDiscoverer(mod)
	mod.Nodes.AddResolver(mod)

	for network, netConfig := range mod.config.Gateway.Networks {
		if netConfig.Endpoint == "" {
			continue
		}
		addr := strings.TrimPrefix(netConfig.Endpoint, network+":")
		endpoint, parseErr := mod.Exonet.Parse(network, addr)
		if parseErr != nil {
			mod.log.Error("invalid gateway endpoint for %v: %v", network, parseErr)
			continue
		}
		mod.configEndpoints[network] = endpoint
	}

	return
}
