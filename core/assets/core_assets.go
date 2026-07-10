package assets

import (
	"errors"
	log2 "github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/resources"
	"gopkg.in/yaml.v2"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"path/filepath"
	"strings"
	"time"
)

var _ Assets = &CoreAssets{}

var dbOpen func(string) gorm.Dialector

const defaultDatabaseName = "astrald"

type CoreAssets struct {
	res resources.Resources
	log *log2.Logger
	db  *gorm.DB
}

// NewCoreAssets opens the default database eagerly; fails if it cannot be reached.
func NewCoreAssets(res resources.Resources, log *log2.Logger) (*CoreAssets, error) {
	var err error
	var a = &CoreAssets{
		res: res,
		log: log,
	}

	a.db, err = a.OpenDatabase(defaultDatabaseName)
	if err != nil {
		return nil, err
	}

	return a, nil
}

func (assets *CoreAssets) Res() resources.Resources {
	return assets.res
}

func (assets *CoreAssets) Read(name string) ([]byte, error) {
	return assets.res.Read(name)
}

func (assets *CoreAssets) Write(name string, data []byte) error {
	return assets.res.Write(name, data)
}

// LoadYAML appends a .yaml suffix to name if missing.
func (assets *CoreAssets) LoadYAML(name string, out interface{}) error {
	if !strings.HasSuffix(strings.ToLower(name), ".yaml") {
		name = name + ".yaml"
	}

	bytes, err := assets.Res().Read(name)
	if err != nil {
		return err
	}

	return yaml.Unmarshal(bytes, out)
}

// StoreYAML appends a .yaml suffix to name if missing.
func (assets *CoreAssets) StoreYAML(name string, in interface{}) error {
	if !strings.HasSuffix(strings.ToLower(name), ".yaml") {
		name = name + ".yaml"
	}

	bytes, err := yaml.Marshal(in)
	if err != nil {
		return err
	}

	return assets.Res().Write(name, bytes)
}

func (assets *CoreAssets) Database() *gorm.DB {
	return assets.db
}

// OpenDatabase resolves name to a file path under FileResources or a shared in-memory db under MemResources.
// Appends a .db suffix if missing; errors when the resource backend supports neither.
func (assets *CoreAssets) OpenDatabase(name string) (*gorm.DB, error) {
	if name == "" {
		return nil, errors.New("invalid name")
	}
	if !strings.HasSuffix(name, ".db") {
		name = name + ".db"
	}

	var l logger.Interface
	if assets.log != nil {
		l = logger.New(
			&logWriter{Logger: assets.log},
			logger.Config{
				SlowThreshold:             200 * time.Millisecond,
				LogLevel:                  logger.Warn,
				IgnoreRecordNotFoundError: false,
				Colorful:                  true,
			})
	} else {
		l = logger.Default.LogMode(logger.Silent)
	}

	var cfg = &gorm.Config{
		Logger: l,
	}

	switch res := assets.res.(type) {
	case *resources.FileResources:
		var dbPath = filepath.Join(res.DataRoot(), name)

		return gorm.Open(
			dbOpen(dbPath),
			cfg,
		)

	case *resources.MemResources:
		var dbPath = "file::memory:?cache=shared"

		return gorm.Open(
			dbOpen(dbPath),
			cfg,
		)
	}

	return nil, errors.New("database unavailable")
}

type logWriter struct {
	*log2.Logger
	level int
}

func (w *logWriter) Printf(s string, i ...interface{}) {
	w.Logger.Logv(w.level, s, i...)
}
