package gateway

import (
	"context"
	"fmt"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
	"time"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/lib/astrald"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/dir"
	"github.com/cryptopunkscc/astrald/mod/gateway"
	gatewayClient "github.com/cryptopunkscc/astrald/mod/gateway/client"
	"github.com/cryptopunkscc/astrald/mod/ip"
	"github.com/cryptopunkscc/astrald/mod/nearby"
	"github.com/cryptopunkscc/astrald/mod/nodes"
	"github.com/cryptopunkscc/astrald/mod/scheduler"
	"github.com/cryptopunkscc/astrald/mod/services"
	"github.com/cryptopunkscc/astrald/mod/tcp"
)

const NetworkName = "gw"

const (
	pingInterval        = 30 * time.Second
	pingTimeout         = 60 * time.Second
	writeTimeout        = 10 * time.Second
	handoffPollInterval = time.Second
	minIdleConns        = 2
	maxDialFails        = 3
	connectTimeout      = 30 * time.Second
	acceptTimeout       = 30 * time.Second
	pipeIdleTimeout     = 24 * time.Hour // note: lower it when we will enforce pings on gw links
)

type Deps struct {
	Dir       dir.Module
	Exonet    exonetmod.Module
	Nearby    nearby.Module
	Nodes     nodes.Module
	Scheduler scheduler.Module
	Services  services.Module
	TCP       tcp.Module
	IP        ip.Module
}

type Module struct {
	Deps

	router routing.OpRouter
	config Config
	node   astral.Node
	log    *log.Logger
	ctx    *astral.Context

	gateways        sig.Set[*astral.Identity]
	registeredNodes sig.Map[string, *registeredNode]
	connectors      sig.Set[*connector]

	configEndpoints map[string]exonet.Endpoint
}

var _ gateway.Module = &Module{}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx.IncludeZone(astral.ZoneNetwork)

	if mod.config.Gateway.Enabled {
		mod.startServers(mod.ctx)
	}

	<-mod.Scheduler.Ready()

	for _, gw := range mod.config.Gateways {
		mod.addPersistentGateway(gw)
	}

	<-ctx.Done()

	// as a gateway
	for _, c := range mod.connectors.Clone() {
		if err := c.Close(); err != nil {
			mod.log.Error("failed to close connector: %v", err)
		}
	}

	for _, b := range mod.registeredNodes.Values() {
		if err := b.Close(); err != nil {
			mod.log.Error("failed to close registered node: %v", err)
		}
	}

	// as client
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, gatewayID := range mod.gateways.Clone() {
		var client = gatewayClient.New(gatewayID, astrald.Default())
		if err := client.Unregister(astral.NewContext(sctx)); err != nil {
			mod.log.Error("failed to unregister from gateway: %v", err)
		}
	}

	return nil
}

// Endpoints returns the module's advertised exonet endpoints.
// note: currently a stub; gateway endpoints are resolved via ResolveEndpoints instead.
func (mod *Module) Endpoints() []exonet.Endpoint {
	var list = make([]exonet.Endpoint, 0)

	return list
}

func (mod *Module) getGatewayEndpoint(network string) (exonet.Endpoint, error) {
	if e, ok := mod.configEndpoints[network]; ok {
		return e, nil
	}

	netConfig, ok := mod.config.Gateway.Networks[network]
	if !ok {
		return nil, fmt.Errorf("no gateway config for network %v", network)
	}

	candidates := mod.IP.PublicIPCandidates()
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no public IP available for gateway network %v", network)
	}

	port := astral.Uint16(netConfig.Port)
	switch network {
	case "tcp":
		return &tcp.Endpoint{IP: candidates[0], Port: port}, nil
	default:
		return nil, fmt.Errorf("unsupported gateway network: %v", network)
	}
}

func (mod *Module) registeredNodeByIdentity(identity *astral.Identity) (*registeredNode, bool) {
	return mod.registeredNodes.Get(identity.String())
}

func (mod *Module) registeredNodeByNonce(nonce astral.Nonce) (*registeredNode, bool) {
	for _, b := range mod.registeredNodes.Values() {
		if b.Nonce == nonce {
			return b, true
		}
	}
	return nil, false
}

func (mod *Module) connectorByNonce(nonce astral.Nonce) (*connector, bool) {
	for _, c := range mod.connectors.Clone() {
		if c.Nonce == nonce {
			return c, true
		}
	}
	return nil, false
}

func (mod *Module) canGateway(identity *astral.Identity) bool {
	return mod.config.Gateway.Enabled
}

func (mod *Module) addPersistentGateway(gatewayID *astral.Identity) {
	mod.gateways.Add(gatewayID)
	mod.Scheduler.Schedule(mod.NewMaintainGatewayConnectionsTask(gatewayID, mod.config.Visibility))
}

func (mod *Module) String() string {
	return gateway.ModuleName
}
