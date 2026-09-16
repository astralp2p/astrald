package tor

import (
	"testing"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core/assets"
	"gopkg.in/yaml.v2"
)

// yamlAssets serves a single tor.yaml from memory.
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
// cross.
func TestLoadSettingsAppliesConfig(t *testing.T) {
	var tests = []struct {
		name   string
		config string
		listen bool
		dial   bool
	}{
		{"defaults", "", true, true},
		{"listen off", "listen: false\n", false, true},
		{"dial off", "dial: false\n", true, false},
		{"listen off, dial on", "listen: false\ndial: true\n", false, true},
		{"both off", "listen: false\ndial: false\n", false, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mod := loadModule(t, test.config)

			if err := mod.loadSettings(astral.NewContext(nil)); err != nil {
				t.Fatalf("loadSettings: %v", err)
			}

			assertFlag(t, "listen", mod.settings.Listen.Get(), test.listen)
			assertFlag(t, "dial", mod.settings.Dial.Get(), test.dial)
		})
	}
}

func assertFlag(t *testing.T, name string, got *astral.Bool, want bool) {
	t.Helper()

	if got == nil {
		t.Fatalf("%v: got unset, want %v", name, want)
	}

	if bool(*got) != want {
		t.Errorf("%v: got %v, want %v", name, bool(*got), want)
	}
}
