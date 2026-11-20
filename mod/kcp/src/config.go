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

	// LinkIdlePing (N) and LinkPingTimeout (T) time the keepalive of links over
	// this transport; a dead link closes within N + T of its last received frame.
	// A non-positive value takes the kcp default below, which equals the kcp
	// TransportProfile in mod/nodes (10 s, 10 s).
	// Values and bounds per transport: astral-docs topics/link-mux.md, Keepalive,
	// "Per-transport values".
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

		// why: a kcp link rides a UDP NAT mapping. On common NATs a fresh
		// mapping survives 15 s of silence but not 30 s, so the probe runs
		// well inside 15 s (astral-docs topics/link-mux.md, Keepalive).
		LinkIdlePing:    10 * time.Second,
		LinkPingTimeout: 10 * time.Second,
	}
}

// linkKeepalive returns the keepalive timing of links over kcp conns.
//
// why: a kcp.yaml key set to 0 or left empty overwrites the default with 0,
// and a 0 left to mod/nodes takes the kcp TransportProfile, which today equals
// the kcp default but is a second copy of it. A non-positive value takes the
// kcp default here instead, so the kcp pair does not depend on that copy.
func (c Config) linkKeepalive() exonetmod.LinkKeepalive {
	def := defaultConfig()
	k := exonetmod.LinkKeepalive{Idle: c.LinkIdlePing, Timeout: c.LinkPingTimeout}
	if k.Idle <= 0 {
		k.Idle = def.LinkIdlePing
	}
	if k.Timeout <= 0 {
		k.Timeout = def.LinkPingTimeout
	}
	return k
}
