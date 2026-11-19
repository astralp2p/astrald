package tcp

import (
	"time"

	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

type Config struct {
	Listen *bool `yaml:"listen,omitempty"`
	Dial   *bool `yaml:"dial,omitempty"`

	Endpoints   []string      `yaml:"configEndpoints,omitempty"`
	DialTimeout time.Duration `yaml:"dial_timeout,omitempty"`
	ListenPort  int           `yaml:"listen_port,omitempty"`

	// LinkIdlePing and LinkPingTimeout time the keepalive of links over this
	// transport (astral-docs topics/link-mux.md, Keepalive).
	LinkIdlePing    time.Duration `yaml:"link_idle_ping,omitempty"`
	LinkPingTimeout time.Duration `yaml:"link_ping_timeout,omitempty"`
}

// defaultConfig returns the config a load starts from.
//
// why: Dial and Listen stay nil unless tcp.yaml sets them, so a value set
// through the tree survives a restart. An unset setting reads as on.
func defaultConfig() Config {
	return Config{
		DialTimeout:     time.Minute,
		ListenPort:      1791,
		LinkIdlePing:    15 * time.Second,
		LinkPingTimeout: 10 * time.Second,
	}
}

// linkKeepalive returns the keepalive timing of links over tcp conns.
func (c Config) linkKeepalive() exonetmod.LinkKeepalive {
	return exonetmod.LinkKeepalive{Idle: c.LinkIdlePing, Timeout: c.LinkPingTimeout}
}
