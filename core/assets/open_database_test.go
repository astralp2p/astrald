package assets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/astralp2p/astrald/lib/paths"
	"gorm.io/gorm"
)

func TestOpenFileDatabaseAtAbsolutePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a b#c", "x.db")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}

	db, err := openFileDatabase(path, &gorm.Config{})
	if err != nil {
		t.Fatalf("openFileDatabase: %v", err)
	}
	var mode string
	if err = db.Raw("PRAGMA journal_mode").Scan(&mode).Error; err != nil || mode != "wal" {
		t.Fatalf("journal_mode = (%q, %v), want wal", mode, err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatalf("database file not at %q: %v", path, err)
	}
}

func TestOpenFileDatabaseRefusesRelativePath(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := openFileDatabase("relnode/astrald.db", &gorm.Config{})
	if !errors.Is(err, paths.ErrNotAbsolute) || !strings.Contains(err.Error(), "relnode/astrald.db") {
		t.Fatalf("openFileDatabase(relative) error = %v, want ErrNotAbsolute naming the path", err)
	}
}

func TestOpenFileDatabaseErrorNamesPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "x.db")

	_, err := openFileDatabase(path, &gorm.Config{})
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("openFileDatabase(missing dir) error = %v, want an error naming %q", err, path)
	}
}
