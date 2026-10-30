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
// why: Listen stays pre-filled, so each start writes tor.yaml's value or the
// default over the tree and a changed default reaches every node.
// why: Dial stays nil unless tor.yaml sets it, so a value set through the tree
// survives a restart. An unset Dial reads as on.
func defaultConfig() Config {
	listen := true

	return Config{
		Listen:      &listen,
		TorProxy:    "127.0.0.1:9050",
		ControlAddr: "127.0.0.1:9051",
		DialTimeout: time.Minute,
		ListenPort:  1791,
	}
}
