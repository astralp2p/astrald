package tor

import (
	"time"

	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

type Config struct {
	Listen *bool `yaml:"listen,omitempty"`
	Dial   *bool `yaml:"dial,omitempty"`

	// TorProxy is Tor's SOCKS port and ControlAddr its control port, each a "host:port" or a
	// "unix:/path" socket. OnionTarget is where the node takes the connections Tor forwards from
	// the onion service: "tcp:127.0.0.1:0" for a free loopback port, or a "unix:/path" socket.
	TorProxy    string        `yaml:"tor_proxy"`
	ControlAddr string        `yaml:"control_addr"`
	OnionTarget string        `yaml:"onion_target"`
	DialTimeout time.Duration `yaml:"dial_timeout"`
	ListenPort  int

	// LinkIdlePing (N) and LinkPingTimeout (T) time the keepalive of links over
	// this transport; a dead link closes within N + T of its last received frame.
	// A 0 leaves the value to the tor TransportProfile in mod/nodes (15 s, 10 s),
	// below the 30 s, 30 s default set here.
	// Values and bounds per transport: astral-docs topics/link-mux.md, Keepalive,
	// "Per-transport values".
	LinkIdlePing    time.Duration `yaml:"link_idle_ping"`
	LinkPingTimeout time.Duration `yaml:"link_ping_timeout"`
}

// defaultConfig returns the config a load starts from.
//
// why: yaml.v2 decodes into an existing non-nil pointer instead of allocating a
// new one, so a default shared by Dial and Listen is rewritten by whichever of
// them tor.yaml sets. Each load gets its own bools.
func defaultConfig() Config {
	// why: an onion service makes the node reachable worldwide, so listen is
	// opt-in through tor.yaml.
	dial, listen := true, false

	return Config{
		Dial:        &dial,
		Listen:      &listen,
		TorProxy:    "127.0.0.1:9050",
		ControlAddr: "127.0.0.1:9051",
		OnionTarget: "tcp:127.0.0.1:0",
		DialTimeout: time.Minute,
		ListenPort:  1791,

		// why: a Tor circuit's round trip spikes to seconds, so a short timeout
		// would close live links.
		LinkIdlePing:    30 * time.Second,
		LinkPingTimeout: 30 * time.Second,
	}
}

// linkKeepalive returns the keepalive timing of links over tor conns.
func (c Config) linkKeepalive() exonetmod.LinkKeepalive {
	return exonetmod.LinkKeepalive{Idle: c.LinkIdlePing, Timeout: c.LinkPingTimeout}
}
