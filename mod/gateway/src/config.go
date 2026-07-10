package gateway

import (
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
)

const defaultGateway = "node1f3AwbE1gJAgAqEx98FMipokcaE9ZapIphzDUkAceE7Pmw8ghmFV19QKCATeC7uyoLszQA"

type NetworkConfig struct {
	Port     int    `yaml:"port"`
	Endpoint string `yaml:"endpoint,omitempty"`
}

type GatewayConfig struct {
	Enabled  bool                      `yaml:"enabled"`
	Networks map[string]*NetworkConfig `yaml:"networks,omitempty"`
}

type Config struct {
	Gateway    GatewayConfig      `yaml:"gateway"`
	Visibility gateway.Visibility `yaml:"visibility"`
	Gateways   []*astral.Identity `yaml:"gateways"`
}

var defaultConfig = Config{
	Visibility: gateway.VisibilityPublic,
}
