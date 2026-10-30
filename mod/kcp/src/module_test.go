package kcp

import (
	"testing"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
	"gopkg.in/yaml.v2"
)

// yamlAssets serves a single kcp.yaml from memory.
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

// loadModule builds a Module from a kcp.yaml body and binds its settings to no
// node, so a Set lands in the Value's local cache and Get reads it back.
//
// why: the logger is silenced, not nil — Load logs the endpoints it cannot
// parse, and logf reads the prefix off the receiver, so a nil logger panics on
// that path.
func loadModule(t *testing.T, config string) *Module {
	t.Helper()

	logger := log.New(nil)
	logger.SetFilter(func(*log.Entry) bool { return false })

	loaded, err := Loader{}.Load(nil, yamlAssets{yaml: config}, logger)
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

// TestLoadSettingsAppliesConfig pins that each flag in kcp.yaml reaches its own
// setting. A listen of false disables the listener, and the two flags never
// cross. A listen stored in the tree survives unless kcp.yaml sets listen.
func TestLoadSettingsAppliesConfig(t *testing.T) {
	off := astral.Bool(false)

	var tests = []struct {
		name   string
		config string
		stored *astral.Bool
		listen bool
		dial   bool
	}{
		{"defaults", "", nil, true, true},
		{"listen off", "listen: false\n", nil, false, true},
		{"dial off", "dial: false\n", nil, true, false},
		{"listen off, dial on", "listen: false\ndial: true\n", nil, false, true},
		{"both off", "listen: false\ndial: false\n", nil, false, false},
		{"stored off survives", "", &off, false, true},
		{"file overrides stored", "listen: true\n", &off, true, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mod := loadModule(t, test.config)
			ctx := astral.NewContext(nil)

			if test.stored != nil {
				if err := mod.settings.Listen.Set(ctx, test.stored); err != nil {
					t.Fatalf("store listen: %v", err)
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
