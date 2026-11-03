package tor

import (
	"errors"
	"net"

	"github.com/astralp2p/astral-go/api/tor"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/resources"
	"golang.org/x/net/proxy"
)

type Loader struct{}

// Load constructs a Module, applies YAML config overrides, and initialises a SOCKS5 proxy targeting TorProxy.
// Fails if the SOCKS5 dialer does not implement proxy.ContextDialer.
func (Loader) Load(node astral.Node, assets assets.Assets, logger *log.Logger) (core.Module, error) {
	mod := &Module{
		node:   node,
		log:    logger,
		assets: assets,
		config: defaultConfig(),
	}

	if err := assets.LoadYAML(tor.ModuleName, &mod.config); err != nil && !errors.Is(err, resources.ErrNotFound) {
		return nil, err
	}

	if err := moveKeyToDataRoot(assets.Res(), logger); err != nil {
		return nil, err
	}

	mod.torServer = NewServer(mod)

	var baseDialer = &net.Dialer{Timeout: mod.config.DialTimeout}

	socksProxy, err := proxy.SOCKS5("tcp", mod.config.TorProxy, nil, baseDialer)
	if err != nil {
		return nil, err
	}

	if dialContext, ok := socksProxy.(proxy.ContextDialer); !ok {
		return nil, errors.New("type cast failed")
	} else {
		mod.proxy = dialContext
	}

	return mod, nil
}

func init() {
	if err := core.RegisterModule(tor.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
