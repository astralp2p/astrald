package resources

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/astralp2p/astrald/lib/paths"
)

func TestNewFileResourcesRefusesRelativeRoot(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := NewFileResources("relnode/config", true)
	if !errors.Is(err, paths.ErrNotAbsolute) || !strings.Contains(err.Error(), "relnode/config") {
		t.Fatalf("NewFileResources(relative) error = %v, want ErrNotAbsolute naming the path", err)
	}
	if _, err := os.Stat("relnode"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("relative root created in the working directory: stat error = %v", err)
	}
}

func TestSetDataRootRefusesRelativePath(t *testing.T) {
	res, err := NewFileResources(t.TempDir(), true)
	if err != nil {
		t.Fatalf("NewFileResources: %v", err)
	}

	err = res.SetDataRoot("reldata")
	if !errors.Is(err, paths.ErrNotAbsolute) || !strings.Contains(err.Error(), "reldata") {
		t.Fatalf("SetDataRoot(relative) error = %v, want ErrNotAbsolute naming the path", err)
	}
	if got := res.DataRoot(); got != res.Root() {
		t.Fatalf("DataRoot() after a refused SetDataRoot = %q, want the root %q", got, res.Root())
	}
}

func TestFileResourcesRefusesNonLocalNames(t *testing.T) {
	dir := t.TempDir()
	res, err := NewFileResources(filepath.Join(dir, "config"), true)
	if err != nil {
		t.Fatalf("NewFileResources: %v", err)
	}
	if err = res.SetDataRoot(filepath.Join(dir, "data")); err != nil {
		t.Fatalf("SetDataRoot: %v", err)
	}

	for _, name := range []string{"../escape", "/etc/passwd", ""} {
		if _, err := res.Read(name); !errors.Is(err, ErrNotLocal) {
			t.Errorf("Read(%q) error = %v, want ErrNotLocal", name, err)
		}
		if _, err := res.ReadData(name); !errors.Is(err, ErrNotLocal) {
			t.Errorf("ReadData(%q) error = %v, want ErrNotLocal", name, err)
		}
		if err := res.Write(name, []byte("x")); !errors.Is(err, ErrNotLocal) {
			t.Errorf("Write(%q) error = %v, want ErrNotLocal", name, err)
		}
		if err := res.WriteData(name, []byte("x")); !errors.Is(err, ErrNotLocal) {
			t.Errorf("WriteData(%q) error = %v, want ErrNotLocal", name, err)
		}
		if err := res.MoveToDataRoot(name); !errors.Is(err, ErrNotLocal) {
			t.Errorf("MoveToDataRoot(%q) error = %v, want ErrNotLocal", name, err)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, "escape")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a write escaped the root: stat error = %v", err)
	}
}
