package gateway

import (
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

type NetworkConfig struct {
	Port     int    `yaml:"port"`
	Endpoint string `yaml:"endpoint,omitempty"`
}

type GatewayConfig struct {
	Enabled  bool                      `yaml:"enabled"`
	Networks map[string]*NetworkConfig `yaml:"networks,omitempty"`
}

// GatewayEntry names one gateway this node registers with.
// Endpoints holds the gateway's own link endpoints as `network:address` strings, e.g. `tcp:203.0.113.7:1791`.
type GatewayEntry struct {
	ID        *astral.Identity `yaml:"id"`
	Endpoints []string         `yaml:"endpoints,omitempty"`
}

type Config struct {
	Gateway    GatewayConfig      `yaml:"gateway"`
	Visibility gateway.Visibility `yaml:"visibility"`
	Gateways   []GatewayEntry     `yaml:"gateways"`

	// LinkIdlePing (N) and LinkPingTimeout (T) time the keepalive of links over
	// this transport; a dead link closes within N + T of its last received frame.
	// A 0 leaves the value to the gw TransportProfile in mod/nodes (15 s, 10 s).
	// Values and bounds per transport: astral-docs topics/link-mux.md, Keepalive,
	// "Per-transport values".
	LinkIdlePing    time.Duration `yaml:"link_idle_ping,omitempty"`
	LinkPingTimeout time.Duration `yaml:"link_ping_timeout,omitempty"`
}

var defaultConfig = Config{
	Visibility: gateway.VisibilityPrivate,

	// why: a gw link crosses the gateway's relay, which adds a hop to every pong.
	LinkIdlePing:    20 * time.Second,
	LinkPingTimeout: 15 * time.Second,
}

// linkKeepalive returns the keepalive timing of links over gw conns.
func (c Config) linkKeepalive() exonetmod.LinkKeepalive {
	return exonetmod.LinkKeepalive{Idle: c.LinkIdlePing, Timeout: c.LinkPingTimeout}
}

// validateGateways checks that every gateways entry names a gateway, and no gateway twice.
// why: the registration task and the registered flag are keyed by the gateway identity.
func (c Config) validateGateways() error {
	seen := map[string]bool{}
	for i, entry := range c.Gateways {
		if entry.ID.IsZero() {
			return fmt.Errorf("gateways[%v]: id is missing", i)
		}
		if seen[entry.ID.String()] {
			return fmt.Errorf("gateways[%v]: gateway %v is listed twice", i, entry.ID)
		}
		seen[entry.ID.String()] = true
	}
	return nil
}
