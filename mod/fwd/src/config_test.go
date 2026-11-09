package fwd

import (
	"testing"

	"gopkg.in/yaml.v2"
)

// TestConfigLoadsTwiceInOneProcess: a second load of an fwd.yaml, as a mobile
// host's restart of its node runs, starts from the defaults, not from the
// first load's forwards.
func TestConfigLoadsTwiceInOneProcess(t *testing.T) {
	const src = "forwards:\n  tcp:127.0.0.1:9000: tcp:127.0.0.1:9001\n"
	for i := 1; i <= 2; i++ {
		cfg := defaultConfig()
		if err := yaml.UnmarshalStrict([]byte(src), &cfg); err != nil {
			t.Fatalf("load %v: %v", i, err)
		}
		if len(cfg.Forwards) != 1 {
			t.Fatalf("load %v: forwards = %v", i, cfg.Forwards)
		}
	}
	if len(defaultConfig().Forwards) != 0 {
		t.Fatalf("loading changed the default forwards: %v", defaultConfig().Forwards)
	}
}
