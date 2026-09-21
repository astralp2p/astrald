package kcp

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/resources"
	"gopkg.in/yaml.v2"
	"gorm.io/gorm"
)

// Load keeps a valid configEndpoints entry and drops an entry ParseEndpoint rejects,
// so no nil *kcp.Endpoint reaches mod.configEndpoints.
func TestLoader_Load_DropsInvalidEndpoint(t *testing.T) {
	const config = `configEndpoints:
  - kcp:10.0.0.1:1792
  - kcp:10.0.0.2
`

	mod := loadModule(t, config)

	if len(mod.configEndpoints) != 1 {
		t.Fatalf("want 1 endpoint, got %d: %v", len(mod.configEndpoints), mod.configEndpoints)
	}
	if mod.configEndpoints[0] == nil {
		t.Fatal("nil endpoint stored")
	}
	if got := mod.configEndpoints[0].Address(); got != "10.0.0.1:1792" {
		t.Fatalf("want 10.0.0.1:1792, got %v", got)
	}
}

func loadModule(t *testing.T, config string) *Module {
	t.Helper()

	logger := log.New(nil)
	logger.SetFilter(func(*log.Entry) bool { return false })

	loaded, err := Loader{}.Load(nil, yamlAssets(config), logger)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	return loaded.(*Module)
}

// yamlAssets serves the module's YAML config from memory. Load reads nothing else.
type yamlAssets string

var _ assets.Assets = yamlAssets("")

var errUnsupported = errors.New("not supported")

func (a yamlAssets) LoadYAML(_ string, out interface{}) error {
	return yaml.Unmarshal([]byte(a), out)
}

func (a yamlAssets) Res() resources.Resources            { return nil }
func (a yamlAssets) Read(string) ([]byte, error)         { return nil, errUnsupported }
func (a yamlAssets) Write(string, []byte) error          { return errUnsupported }
func (a yamlAssets) StoreYAML(string, interface{}) error { return errUnsupported }
func (a yamlAssets) Database() *gorm.DB                  { return nil }
