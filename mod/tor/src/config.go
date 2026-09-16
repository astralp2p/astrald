package tor

import "time"

type Config struct {
	Listen *bool `yaml:"listen,omitempty"`
	Dial   *bool `yaml:"dial,omitempty"`

	TorProxy    string        `yaml:"tor_proxy"`
	ControlAddr string        `yaml:"control_addr"`
	DialTimeout time.Duration `yaml:"dial_timeout"`
	ListenPort  int
}

// defaultConfig returns the config a load starts from.
//
// why: yaml.v2 decodes into an existing non-nil pointer instead of allocating a
// new one, so a default shared by Dial and Listen is rewritten by whichever of
// them tor.yaml sets. Each load gets its own bools.
func defaultConfig() Config {
	dial, listen := true, true

	return Config{
		Dial:        &dial,
		Listen:      &listen,
		TorProxy:    "127.0.0.1:9050",
		ControlAddr: "127.0.0.1:9051",
		DialTimeout: time.Minute,
		ListenPort:  1791,
	}
}
