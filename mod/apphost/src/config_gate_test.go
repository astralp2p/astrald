package apphost

import (
	"slices"
	"testing"

	"github.com/astralp2p/astrald/lib/policyconf"
	"gopkg.in/yaml.v2"
)

// loadConfig reads src over the defaults as Loader.Load does.
func loadConfig(t *testing.T, src string) (Config, error) {
	t.Helper()
	cfg := defaultConfig
	err := yaml.UnmarshalStrict([]byte(src), &cfg)
	return cfg, err
}

// TestConfigDefaultsMatchTheDistribution: the code defaults are the
// distribution's apphost.yaml.
func TestConfigDefaultsMatchTheDistribution(t *testing.T) {
	distribution := `
gate:
  tokenless: [.spec, apphost.register, sys.register_manager, user.info]
  states:
    fresh: [.spec, sys.register_manager, user.info]
    unclaimed: [.spec, user.info, user.derive_identity, user.bootstrap, user.join, apphost.register,
      apphost.bind, apphost.register_handler, apphost.cancel]
    claimed: ["*"]
policies:
  app_register: reject_all
`
	var got Config
	if err := yaml.UnmarshalStrict([]byte(distribution), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !slices.Equal(got.Gate.Tokenless, defaultConfig.Gate.Tokenless) ||
		!slices.Equal(got.Gate.States.Fresh, defaultConfig.Gate.States.Fresh) ||
		!slices.Equal(got.Gate.States.Unclaimed, defaultConfig.Gate.States.Unclaimed) ||
		!slices.Equal(got.Gate.States.Claimed, defaultConfig.Gate.States.Claimed) ||
		got.Policies.AppRegister != defaultConfig.Policies.AppRegister {
		t.Fatalf("distribution config %+v; code defaults %+v", got.Gate, defaultConfig.Gate)
	}
}

// TestConfigReplacesOnlySuppliedKeys: the E2E override of tokenless and fresh
// keeps the default unclaimed and claimed lists.
func TestConfigReplacesOnlySuppliedKeys(t *testing.T) {
	cfg, err := loadConfig(t, `
gate:
  tokenless: [apphost.register, user.info, user.derive_identity, user.bootstrap, user.join]
  states:
    fresh: ["*"]
policies:
  app_register: accept_all
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !slices.Equal(cfg.Gate.States.Fresh, []string{gateAny}) || len(cfg.Gate.Tokenless) != 5 {
		t.Fatalf("supplied keys not applied: %+v", cfg.Gate)
	}
	if !slices.Equal(cfg.Gate.States.Unclaimed, defaultConfig.Gate.States.Unclaimed) ||
		!slices.Equal(cfg.Gate.States.Claimed, defaultConfig.Gate.States.Claimed) {
		t.Fatalf("omitted keys lost their defaults: %+v", cfg.Gate.States)
	}
	if !cfg.Policies.AppRegister.Accepts() {
		t.Fatal("policies.app_register: accept_all not applied")
	}
	if defaultConfig.Policies.AppRegister.Mode != policyconf.RejectAll || len(defaultConfig.Gate.States.Fresh) != 3 {
		t.Fatal("loading changed the defaults")
	}
}
