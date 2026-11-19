package gateway

import (
	"fmt"

	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
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
}

var defaultConfig = Config{
	Visibility: gateway.VisibilityPrivate,
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
