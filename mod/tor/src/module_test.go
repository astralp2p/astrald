package tor

import (
	"testing"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/resources"
	"gopkg.in/yaml.v2"
)

// yamlAssets serves a single tor.yaml from memory, with memory resources behind it.
//
// why: the embedded nil interface satisfies assets.Assets without implementing
// it. Any method other than LoadYAML and Res panics, which is the assertion that
// Load reads its config file and moves tor.key, and nothing else.
type yamlAssets struct {
	assets.Assets
	yaml string
}

func (a yamlAssets) LoadYAML(_ string, out any) error {
	return yaml.Unmarshal([]byte(a.yaml), out)
}

func (a yamlAssets) Res() resources.Resources { return resources.NewMemResources() }

// loadModule builds a Module from a tor.yaml body and binds its settings to no
// node, so a Set lands in the Value's local cache and Get reads it back.
func loadModule(t *testing.T, config string) *Module {
	t.Helper()

	loaded, err := Loader{}.Load(nil, yamlAssets{yaml: config}, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mod := loaded.(*Module)
	mod.settings = Settings{
		Listen: &tree.Value[*astral.Bool]{},
		Dial:   &tree.Value[*astral.Bool]{},
	}

	return mod
}

// TestLoadSettingsAppliesConfig pins that each flag in tor.yaml reaches its own
// setting. A listen of false disables the listener, and the two flags never
// cross. A dial stored in the tree survives unless tor.yaml sets dial. A stored
// listen never survives: tor.yaml or the default replaces it at every start.
func TestLoadSettingsAppliesConfig(t *testing.T) {
	off := astral.Bool(false)

	var tests = []struct {
		name         string
		config       string
		storedListen *astral.Bool
		storedDial   *astral.Bool
		listen       bool
		dial         bool
	}{
		{"defaults", "", nil, nil, true, true},
		{"listen off", "listen: false\n", nil, nil, false, true},
		{"dial off", "dial: false\n", nil, nil, true, false},
		{"listen off, dial on", "listen: false\ndial: true\n", nil, nil, false, true},
		{"both off", "listen: false\ndial: false\n", nil, nil, false, false},
		{"stored dial off survives", "", nil, &off, true, false},
		{"file overrides stored dial", "dial: true\n", nil, &off, true, true},
		{"default overrides stored listen", "", &off, nil, true, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mod := loadModule(t, test.config)
			ctx := astral.NewContext(nil)

			if test.storedListen != nil {
				if err := mod.settings.Listen.Set(ctx, test.storedListen); err != nil {
					t.Fatalf("store listen: %v", err)
				}
			}

			if test.storedDial != nil {
				if err := mod.settings.Dial.Set(ctx, test.storedDial); err != nil {
					t.Fatalf("store dial: %v", err)
				}
			}

			if err := mod.loadSettings(ctx); err != nil {
				t.Fatalf("loadSettings: %v", err)
			}

			assertFlag(t, "listen", mod.settings.Listen.Get(), test.listen)
			assertFlag(t, "dial", mod.settings.Dial.Get(), test.dial)
		})
	}
}

// assertFlag reads an unset flag as on, the default every reader applies.
func assertFlag(t *testing.T, name string, got *astral.Bool, want bool) {
	t.Helper()

	if (got == nil || bool(*got)) != want {
		t.Errorf("%v: got %v, want %v", name, got, want)
	}
}
