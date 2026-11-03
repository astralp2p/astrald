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

// TestSyncConfigKeepsStoredMode pins that a start writes the mode only when
// nearby.yaml sets it: an unset mode stays unset, and a mode stored in the tree
// survives unless the file sets mode.
func TestSyncConfigKeepsStoredMode(t *testing.T) {
	silent, visible := nearby.ModeSilent, nearby.ModeVisible

	// want is the stored mode the start leaves; nil means unset.
	var tests = []struct {
		name   string
		config string
		stored *nearby.Mode
		want   *nearby.Mode
	}{
		{"unset stays unset", "", nil, nil},
		{"stored mode survives", "", &visible, &visible},
		{"file overrides stored", "mode: 1\n", &silent, &visible},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loaded, err := Loader{}.Load(nil, yamlAssets{yaml: test.config}, nil)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			mod := loaded.(*Module)
			ctx := astral.NewContext(nil)

			if test.stored != nil {
				if err := mod.SetMode(ctx, *test.stored); err != nil {
					t.Fatalf("store mode: %v", err)
				}
			}

			if err := mod.syncConfig(ctx); err != nil {
				t.Fatalf("syncConfig: %v", err)
			}

			got := mod.mode.Get()
			if test.want == nil {
				if got != nil {
					t.Errorf("mode: got %v, want unset", *got)
				}
				return
			}

			if got == nil {
				t.Fatalf("mode: got unset, want %v", *test.want)
			}

			if *got != *test.want {
				t.Errorf("mode: got %v, want %v", *got, *test.want)
			}
		})
	}
}
