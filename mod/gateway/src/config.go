package gateway

import (
	"time"

	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
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

	// LinkIdlePing and LinkPingTimeout time the keepalive of links over this
	// transport (astral-docs topics/link-mux.md, Keepalive).
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
