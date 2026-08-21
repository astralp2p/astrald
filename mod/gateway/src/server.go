package gateway

import (
	"github.com/astralp2p/astral-go/astral"
)

func (mod *Module) startServers(ctx *astral.Context) {
	for network, netConfig := range mod.config.Gateway.Networks {
		port := astral.Uint16(netConfig.Port)
		switch network {
		case "tcp":
			mod.log.Logv(1, "start listening on tcp port %v", port)
			if err := mod.TCP.CreateEphemeralListener(ctx, port, mod.handleInbound); err != nil {
				mod.log.Error("tcp listen on port %v: %v", port, err)
			}
		default:
			mod.log.Error("unsupported gateway network: %v", network)
		}
	}
}
