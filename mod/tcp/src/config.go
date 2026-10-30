package tcp

import "time"

type Config struct {
	Listen *bool `yaml:"listen,omitempty"`
	Dial   *bool `yaml:"dial,omitempty"`

	Endpoints   []string      `yaml:"configEndpoints,omitempty"`
	DialTimeout time.Duration `yaml:"dial_timeout,omitempty"`
	ListenPort  int           `yaml:"listen_port,omitempty"`
}

// defaultConfig returns the config a load starts from.
//
// why: Dial and Listen stay nil unless tcp.yaml sets them, so a value set
// through the tree survives a restart. An unset setting reads as on.
func defaultConfig() Config {
	return Config{
		DialTimeout: time.Minute,
		ListenPort:  1791,
	}
}
