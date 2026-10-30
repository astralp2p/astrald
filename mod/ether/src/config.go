package ether

const etherUDPPort = 8822

type Config struct {
	// note: false skips the UDP bind, so ether runs as a no-op.
	Listen  bool `yaml:"listen"`
	UDPPort int  `yaml:"udp_port,omitempty"`
}

var defaultConfig = Config{
	Listen:  true,
	UDPPort: etherUDPPort,
}
