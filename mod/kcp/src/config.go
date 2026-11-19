package kcp

import (
	"time"

	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

type Config struct {
	Listen *bool `yaml:"listen,omitempty"`
	Dial   *bool `yaml:"dial,omitempty"`

	Endpoints   []string      `yaml:"configEndpoints,omitempty"`
	ListenPort  int           `yaml:"listen_port,omitempty"`
	DialTimeout time.Duration `yaml:"dial_timeout,omitempty"`

	// EphemeralIdleTimeout closes an ephemeral listener that accepts no
	// connection within it. Zero disables the timeout.
	EphemeralIdleTimeout time.Duration `yaml:"ephemeral_idle_timeout,omitempty"`

	// LinkIdlePing and LinkPingTimeout time the keepalive of links over this
	// transport (astral-docs topics/link-mux.md, Keepalive).
	LinkIdlePing    time.Duration `yaml:"link_idle_ping,omitempty"`
	LinkPingTimeout time.Duration `yaml:"link_ping_timeout,omitempty"`
}

// defaultConfig returns the config a load starts from.
//
// why: Dial and Listen stay nil unless kcp.yaml sets them, so a value set
// through the tree survives a restart. An unset setting reads as on.
func defaultConfig() Config {
	return Config{
		// why: DialTimeout bounds only the handshake, which takes milliseconds
		// on a LAN and under a second on a punched NAT path; a dial to a dead
		// address fails after it.
		DialTimeout: 5 * time.Second,
		ListenPort:  1792,

		// why: a traversal that fails after the peer opened a listener never sends a
		// connection, and nothing else reclaims the port. The window is wide enough
		// that a slow but live traversal still arrives first.
		EphemeralIdleTimeout: 15 * time.Minute,

		LinkIdlePing:    15 * time.Second,
		LinkPingTimeout: 10 * time.Second,
	}
}

// linkKeepalive returns the keepalive timing of links over kcp conns.
func (c Config) linkKeepalive() exonetmod.LinkKeepalive {
	return exonetmod.LinkKeepalive{Idle: c.LinkIdlePing, Timeout: c.LinkPingTimeout}
}
