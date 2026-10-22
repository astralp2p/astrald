package coldcard

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/glebarez/sqlite"
	"gopkg.in/yaml.v2"
	"gorm.io/gorm"
)

// yamlAssets serves one coldcard.yaml body from memory over an empty store.
//
// why: the embedded nil interface satisfies assets.Assets without implementing
// it. Any method other than LoadYAML and Database panics, which is the assertion
// that Load reads its config file and nothing else.
type yamlAssets struct {
	assets.Assets
	yaml string
	db   *gorm.DB
}

func (a yamlAssets) LoadYAML(_ string, out any) error {
	return yaml.Unmarshal([]byte(a.yaml), out)
}

func (a yamlAssets) Database() *gorm.DB { return a.db }

// loadModule builds a Module from a coldcard.yaml body.
func loadModule(t *testing.T, config string) *Module {
	t.Helper()

	loaded, err := Loader{}.Load(nil, yamlAssets{yaml: config, db: openStore(t)}, log.New(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return loaded.(*Module)
}

// openStore opens an empty in-memory store and closes it when the test ends.
//
// why the pool is capped at one connection: an in-memory sqlite database belongs
// to the connection that opened it, so a second pooled connection is a second,
// empty database.
func openStore(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("reach store: %v", err)
	}

	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })

	return db
}

// TestLoadWiresTheCkccBackend pins the production default: the loader gives the
// module the ckcc backend, so no caller chooses an implementation.
func TestLoadWiresTheCkccBackend(t *testing.T) {
	backend := loadModule(t, "").backend
	if _, ok := backend.(ckccBackend); !ok {
		t.Fatalf("Loader wired backend %T; want ckccBackend", backend)
	}
}

// TestLoadScanInterval covers the config surface: a configured interval is kept,
// and an absent or non-positive one falls back to the default.
func TestLoadScanInterval(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		want   time.Duration
	}{
		{"configured", "scan_interval: 90s\n", 90 * time.Second},
		{"absent", "", defaultConfig.ScanInterval},
		{"zero", "scan_interval: 0s\n", defaultConfig.ScanInterval},
		{"negative", "scan_interval: -1s\n", defaultConfig.ScanInterval},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := loadModule(t, tc.config).scanInterval(); got != tc.want {
				t.Fatalf("scanInterval() = %v; want %v", got, tc.want)
			}
		})
	}
}
