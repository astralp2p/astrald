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
// why: yaml.v2 decodes into an existing non-nil pointer instead of allocating a
// new one, so a default shared by Dial and Listen is rewritten by whichever of
// them tcp.yaml sets. Each load gets its own bools.
func defaultConfig() Config {
	dial, listen := true, true

	return Config{
		Dial:        &dial,
		Listen:      &listen,
		DialTimeout: time.Minute,
		ListenPort:  1791,
	}
}
