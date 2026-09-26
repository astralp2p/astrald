package messaging

import (
	"testing"
	"time"

	"gopkg.in/yaml.v2"
)

// yamlAssets serves one messaging.yaml body from memory, over the test's store.
type yamlAssets struct {
	testAssets
	yaml string
}

func (a yamlAssets) LoadYAML(_ string, out any) error {
	return yaml.Unmarshal([]byte(a.yaml), out)
}

// loadConfigured loads the module over a fresh store with body as its
// messaging.yaml.
func loadConfigured(t *testing.T, body string) *Module {
	t.Helper()

	loaded, err := Loader{}.Load(nil, yamlAssets{testAssets: testAssets{db: testDB(t).DB}, yaml: body}, testLogger())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return loaded.(*Module)
}

// Load gives a hosting_duration or a renewal_interval of zero or less its
// default, and keeps a positive one.
func TestLoadGivesANonPositiveDurationItsDefault(t *testing.T) {
	hosting, renewal := defaultConfig.HostingDuration, defaultConfig.RenewalInterval
	cases := []struct {
		name, yaml       string
		hosting, renewal time.Duration
	}{
		{"zero hosting", "hosting_duration: 0s", hosting, renewal},
		{"negative hosting", "hosting_duration: -1h", hosting, renewal},
		{"zero interval", "renewal_interval: 0s", hosting, renewal},
		{"negative interval", "renewal_interval: -1m", hosting, renewal},
		{"positive", "hosting_duration: 2h\nrenewal_interval: 5m", 2 * time.Hour, 5 * time.Minute},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config := loadConfigured(t, c.yaml).config
			if config.HostingDuration != c.hosting || config.RenewalInterval != c.renewal {
				t.Fatalf("%q loaded hosting_duration %v and renewal_interval %v, want %v and %v",
					c.yaml, config.HostingDuration, config.RenewalInterval, c.hosting, c.renewal)
			}
		})
	}
}
