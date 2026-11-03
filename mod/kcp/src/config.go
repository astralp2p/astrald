package kcp

import (
	"time"
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
}

// defaultConfig returns the config a load starts from.
//
// why: Dial and Listen stay nil unless kcp.yaml sets them, so a value set
// through the tree survives a restart. An unset setting reads as on.
func defaultConfig() Config {
	return Config{
		DialTimeout: time.Minute,
		ListenPort:  1792,

		// why: a traversal that fails after the peer opened a listener never sends a
		// connection, and nothing else reclaims the port. The window is wide enough
		// that a slow but live traversal still arrives first.
		EphemeralIdleTimeout: 15 * time.Minute,
	}
}
