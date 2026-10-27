package assets

import (
	"errors"
	"testing"

	"github.com/astralp2p/astrald/lib/paths"
)

func TestDBDSNAbsolutePath(t *testing.T) {
	const path = "/tmp/a b/#x%20.db"

	got, err := dbDSN(path)
	if want := path + "?" + dbSettings; err != nil || got != want {
		t.Fatalf("dbDSN(%q) = (%q, %v), want (%q, nil)", path, got, err, want)
	}
}

func TestDBDSNRefusesRelativePath(t *testing.T) {
	if _, err := dbDSN("relnode/data/astrald.db"); !errors.Is(err, paths.ErrNotAbsolute) {
		t.Fatalf("dbDSN(relative) error = %v, want %v", err, paths.ErrNotAbsolute)
	}
}

func TestDBDSNRefusesQuestionMark(t *testing.T) {
	if _, err := dbDSN("/tmp/a?b/astrald.db"); !errors.Is(err, errQueryInPath) {
		t.Fatalf("dbDSN(path with ?) error = %v, want %v", err, errQueryInPath)
	}
}
