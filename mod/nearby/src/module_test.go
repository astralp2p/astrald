package nearby

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
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

func modePtr(m nearby.Mode) *nearby.Mode { return &m }

// SetMode refuses a mode above Stealth and keeps the stored one.
func TestSetModeRefusesAModeAboveStealth(t *testing.T) {
	h := newHarness(t, astral.GenerateIdentity())
	h.setMode(t, nearby.ModeVisible)

	for _, m := range []nearby.Mode{3, 255} {
		if err := h.mod.SetMode(astral.NewContext(nil), m); !errors.Is(err, nearbymod.ErrInvalidMode) {
			t.Fatalf("SetMode(%v) = %v; want %v", m, err, nearbymod.ErrInvalidMode)
		}
	}
	if got := h.mod.Mode(); got != nearby.ModeVisible {
		t.Fatalf("Mode() = %v after refused SetMode; want %v", got, nearby.ModeVisible)
	}
}

// A mode above Stealth in nearby.yaml fails the load of the module.
func TestLoadRefusesAYamlModeAboveStealth(t *testing.T) {
	cases := []struct {
		yaml string
		ok   bool
	}{
		{"mode: 3\n", false},
		{"mode: 2\n", true},
		{"mode: 0\nsilent_listen: false\n", true},
		{"", true},
	}

	for _, tc := range cases {
		_, err := Loader{}.Load(nil, yamlAssets{yaml: tc.yaml}, nil)
		switch {
		case tc.ok && err != nil:
			t.Errorf("Load(%q) = %v; want nil", tc.yaml, err)
		case !tc.ok && !errors.Is(err, nearbymod.ErrInvalidMode):
			t.Errorf("Load(%q) = %v; want %v", tc.yaml, err, nearbymod.ErrInvalidMode)
		}
	}
}

// A mode above Stealth written straight into the tree reads as unset: Visible while
// unowned and Stealth once owned.
func TestStoredModeAboveStealthReadsUnset(t *testing.T) {
	for _, tc := range []struct {
		owner *astral.Identity
		want  nearby.Mode
	}{{nil, nearby.ModeVisible}, {astral.GenerateIdentity(), nearby.ModeStealth}} {
		h := newHarness(t, tc.owner)
		root := newMemNode()
		if err := root.at(t, modePath).Set(astral.NewContext(nil), modePtr(3)); err != nil {
			t.Fatalf("store mode: %v", err)
		}
		if err := h.mod.bindSettings(astral.NewContext(nil), root); err != nil {
			t.Fatalf("bindSettings: %v", err)
		}

		if got := h.mod.Mode(); got != tc.want {
			t.Fatalf("owner %v: Mode() = %v; want %v", tc.owner, got, tc.want)
		}
		if !h.mod.badModeLogged.Load() {
			t.Fatal("the invalid stored mode was not logged")
		}
	}
}

// The mode before and after the claim: an unset mode is Visible while unowned and
// Stealth once owned; a stored Visible survives the claim; a stored Silent holds before
// it; a stored Stealth reads Visible until the claim.
func TestModeOnClaim(t *testing.T) {
	silent, visible, stealth := nearby.ModeSilent, nearby.ModeVisible, nearby.ModeStealth
	cases := []struct {
		name           string
		stored         *nearby.Mode
		unowned, owned nearby.Mode
	}{
		{"unset", nil, visible, stealth},
		{"stored visible", &visible, visible, visible},
		{"stored silent", &silent, silent, silent},
		{"stored stealth", &stealth, visible, stealth},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			if tc.stored != nil {
				h.setMode(t, *tc.stored)
			}
			if got := h.mod.Mode(); got != tc.unowned {
				t.Fatalf("unowned: Mode() = %v; want %v", got, tc.unowned)
			}

			h.mod.User = testUser{id: astral.GenerateIdentity()}
			if got := h.mod.Mode(); got != tc.owned {
				t.Fatalf("owned: Mode() = %v; want %v", got, tc.owned)
			}
		})
	}
}

// bindSettings writes the mode and silent_listen nearby.yaml states into the tree, and
// keeps the stored values the file leaves out.
func TestBindSettingsSyncsTheConfigIntoTheTree(t *testing.T) {
	off := false
	cases := []struct {
		name         string
		config       Config
		storedMode   *nearby.Mode
		storedListen *astral.Bool
		mode         nearby.Mode
		listen       bool
	}{
		{"file states both", Config{Mode: modePtr(nearby.ModeSilent), SilentListen: &off}, modePtr(nearby.ModeVisible), nil, nearby.ModeSilent, false},
		{"file states none", Config{}, modePtr(nearby.ModeVisible), boolPtr(false), nearby.ModeVisible, false},
		{"nothing stored", Config{}, nil, nil, nearby.ModeStealth, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, astral.GenerateIdentity())
			h.mod.config = tc.config
			root := newMemNode()
			ctx := astral.NewContext(nil)
			if tc.storedMode != nil {
				_ = root.at(t, modePath).Set(ctx, tc.storedMode)
			}
			if tc.storedListen != nil {
				_ = root.at(t, silentListenPath).Set(ctx, tc.storedListen)
			}

			if err := h.mod.bindSettings(ctx, root); err != nil {
				t.Fatalf("bindSettings: %v", err)
			}
			if got := h.mod.Mode(); got != tc.mode {
				t.Errorf("Mode() = %v; want %v", got, tc.mode)
			}
			if got := h.mod.listensInSilent(); got != tc.listen {
				t.Errorf("silent_listen = %v; want %v", got, tc.listen)
			}
			requireTreeSettings(t, root, tc.config)
		})
	}
}

func boolPtr(v bool) *astral.Bool { b := astral.Bool(v); return &b }

// requireTreeSettings checks the tree holds each value config states.
func requireTreeSettings(t *testing.T, root *memNode, config Config) {
	t.Helper()
	if config.Mode != nil {
		if m, ok := root.at(t, modePath).value.(*nearby.Mode); !ok || *m != *config.Mode {
			t.Errorf("tree %v holds %v; want %v", modePath, root.at(t, modePath).value, *config.Mode)
		}
	}
	if config.SilentListen != nil {
		if b, ok := root.at(t, silentListenPath).value.(*astral.Bool); !ok || bool(*b) != *config.SilentListen {
			t.Errorf("tree %v holds %v; want %v", silentListenPath, root.at(t, silentListenPath).value, *config.SilentListen)
		}
	}
}

// plugin composes or recognizes statuses, or both.
type plugin struct {
	core.Module
	*modeComposer
	*fakeRecognizer
}

// addPlugins registers the composers and recognizers among the loaded modules, and
// never nearby itself.
func TestAddPluginsCollectsComposersAndRecognizers(t *testing.T) {
	h := newHarness(t, nil)
	composer := &struct {
		core.Module
		*modeComposer
	}{modeComposer: &modeComposer{mod: h.mod}}
	recognizer := &struct {
		core.Module
		*fakeRecognizer
	}{fakeRecognizer: fixedRecognizer(nil, nil)}
	both := &plugin{modeComposer: &modeComposer{mod: h.mod}, fakeRecognizer: fixedRecognizer(nil, nil)}
	neither := &struct{ core.Module }{}

	h.mod.addPlugins([]core.Module{h.mod, composer, recognizer, both, neither})

	if n := h.mod.composers.Count(); n != 2 {
		t.Errorf("registered %d composers; want 2", n)
	}
	if n := h.mod.recognizers.Count(); n != 2 {
		t.Errorf("registered %d recognizers; want 2", n)
	}
}
