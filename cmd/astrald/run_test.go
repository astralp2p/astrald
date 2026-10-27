package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/debug"
	"github.com/astralp2p/astrald/resources"
)

// keepLogDir restores the crash-log directory after the test.
func keepLogDir(t *testing.T) {
	prev := debug.LogDir()
	t.Cleanup(func() { debug.SetLogDir(prev) })
}

// startOldLayout writes node_key the way a version that kept it in the config root did.
func startOldLayout(t *testing.T, root string) *astral.Identity {
	t.Helper()
	old, err := resources.NewFileResources(filepath.Join(root, "config"), true)
	if err != nil {
		t.Fatal(err)
	}
	id, err := loadNodeIdentity(old)
	if err != nil {
		t.Fatalf("loadNodeIdentity in the old layout: %v", err)
	}
	return id
}

// startNode runs setupResources and loadNodeIdentity on root and returns the identity and stderr.
func startNode(t *testing.T, root string) (*astral.Identity, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := os.Stderr
	os.Stderr = w
	res, err := setupResources(&Args{Root: root})
	os.Stderr = stderr
	w.Close()
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("setupResources: %v", err)
	}

	id, err := loadNodeIdentity(res)
	if err != nil {
		t.Fatalf("loadNodeIdentity: %v", err)
	}
	return id, string(out)
}

func statKey(t *testing.T, dir string) error {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, resNodeKey))
	return err
}

func TestSetupResourcesFreshRoot(t *testing.T) {
	keepLogDir(t)
	root := t.TempDir()

	startNode(t, root)

	if err := statKey(t, filepath.Join(root, "data")); err != nil {
		t.Fatalf("node_key not in the data root: %v", err)
	}
	if err := statKey(t, filepath.Join(root, "config")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("node_key in the config root: stat error = %v, want fs.ErrNotExist", err)
	}
	if want := filepath.Join(root, "data"); debug.LogDir() != want {
		t.Fatalf("debug.LogDir() = %q, want the data root %q", debug.LogDir(), want)
	}
}

func TestSetupResourcesMovesNodeKey(t *testing.T) {
	keepLogDir(t)
	root := t.TempDir()
	want := startOldLayout(t, root)

	got, stderr := startNode(t, root)

	if !got.IsEqual(want) {
		t.Fatalf("identity after upgrade = %v, want %v", got, want)
	}
	if err := statKey(t, filepath.Join(root, "config")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("node_key still in the config root: stat error = %v", err)
	}
	if err := statKey(t, filepath.Join(root, "data")); err != nil {
		t.Fatalf("node_key not in the data root: %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

// TestSetupResourcesReadOnlyConfigKeepsIdentity: a config root that refuses
// the removal keeps the identity and prints a warning naming the old copy.
func TestSetupResourcesReadOnlyConfigKeepsIdentity(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	keepLogDir(t)
	root := t.TempDir()
	want := startOldLayout(t, root)
	cfg := filepath.Join(root, "config")
	if err := os.Chmod(cfg, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(cfg, 0700) })

	got, stderr := startNode(t, root)

	if !got.IsEqual(want) {
		t.Fatalf("identity after upgrade = %v, want %v", got, want)
	}
	if err := statKey(t, filepath.Join(root, "data")); err != nil {
		t.Fatalf("node_key not in the data root: %v", err)
	}
	oldPath := filepath.Join(cfg, resNodeKey)
	if !strings.HasPrefix(stderr, "warning: ") || !strings.Contains(stderr, oldPath) {
		t.Fatalf("stderr = %q, want a warning naming %s", stderr, oldPath)
	}
}

// TestSetupResourcesCrashLogInDataRoot: a crash after setup writes its log to
// the data root and nothing to the config root.
func TestSetupResourcesCrashLogInDataRoot(t *testing.T) {
	keepLogDir(t)
	root := t.TempDir()
	startNode(t, root)

	func() {
		defer func() { recover() }()
		defer debug.SaveLog(nil)
		panic("boom")
	}()

	for dir, want := range map[string]int{"data": 1, "config": 0} {
		logs, err := filepath.Glob(filepath.Join(root, dir, "crash.*.log"))
		if err != nil || len(logs) != want {
			t.Fatalf("crash logs in %s = (%v, %v), want %d", dir, logs, err, want)
		}
	}
}
