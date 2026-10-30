package nearby

import (
	"testing"

	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core/assets"
	"gopkg.in/yaml.v2"
)

// yamlAssets serves a single nearby.yaml from memory.
//
// why: the embedded nil interface satisfies assets.Assets without implementing
// it. Any method other than LoadYAML panics, which is the assertion that Load
// reads its config file and nothing else.
type yamlAssets struct {
	assets.Assets
	yaml string
}

func (a yamlAssets) LoadYAML(_ string, out any) error {
	return yaml.Unmarshal([]byte(a.yaml), out)
}

// TestSyncConfigKeepsStoredMode pins that a mode stored in the tree survives a
// start unless nearby.yaml sets mode.
func TestSyncConfigKeepsStoredMode(t *testing.T) {
	var tests = []struct {
		name   string
		config string
		want   nearby.Mode
	}{
		{"stored mode survives", "", nearby.ModeSilent},
		{"file overrides stored", "mode: 1\n", nearby.ModeVisible},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loaded, err := Loader{}.Load(nil, yamlAssets{yaml: test.config}, nil)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			mod := loaded.(*Module)
			ctx := astral.NewContext(nil)

			if err := mod.SetMode(ctx, nearby.ModeSilent); err != nil {
				t.Fatalf("store mode: %v", err)
			}

			if err := mod.syncConfig(ctx); err != nil {
				t.Fatalf("syncConfig: %v", err)
			}

			got := mod.mode.Get()
			if got == nil {
				t.Fatalf("mode: got unset, want %v", test.want)
			}

			if *got != test.want {
				t.Errorf("mode: got %v, want %v", *got, test.want)
			}
		})
	}
}
