package tor

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	alog "github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/resources"
)

// keyAssets serves tor.key reads from a fixed result and records writes.
//
// why: the embedded nil interface satisfies assets.Assets without implementing
// it, so any other method panics, including the config-root Read and Write.
// The Module has no torServer, so reaching generatePrivateKey panics too: both
// are assertions that getPrivateKey only reads the data root.
type keyAssets struct {
	assets.Assets
	data   []byte
	err    error
	writes int
}

func (a *keyAssets) ReadData(string) ([]byte, error) { return a.data, a.err }

func (a *keyAssets) WriteData(string, []byte) error {
	a.writes++
	return nil
}

// TestGetPrivateKeyReturnsStoredKey pins that a readable tor.key is used as is.
func TestGetPrivateKeyReturnsStoredKey(t *testing.T) {
	stored := Key{1, 2, 3}
	a := &keyAssets{data: stored}
	mod := &Module{assets: a}

	key, err := mod.getPrivateKey()
	if err != nil {
		t.Fatalf("getPrivateKey: %v", err)
	}
	if !bytes.Equal(key, stored) {
		t.Errorf("getPrivateKey = %x, want %x", key, stored)
	}
	if a.writes != 0 {
		t.Errorf("writes = %d, want 0", a.writes)
	}
}

// TestGetPrivateKeyReadErrorKeepsKey: a read error other than not-found must be
// returned without generating or writing a key, so the onion address cannot
// change because tor.key was unreadable.
func TestGetPrivateKeyReadErrorKeepsKey(t *testing.T) {
	readErr := fs.ErrPermission
	a := &keyAssets{err: readErr}
	mod := &Module{assets: a}

	key, err := mod.getPrivateKey()
	if !errors.Is(err, readErr) {
		t.Fatalf("getPrivateKey error = %v, want %v", err, readErr)
	}
	if key != nil {
		t.Errorf("getPrivateKey key = %x, want nil", key)
	}
	if a.writes != 0 {
		t.Errorf("writes = %d, want 0", a.writes)
	}
}

// dataAssets serves the data-root pair from res; any other method panics.
type dataAssets struct {
	assets.Assets
	res resources.Resources
}

func (a dataAssets) ReadData(name string) ([]byte, error) { return a.res.ReadData(name) }

func (a dataAssets) WriteData(name string, data []byte) error { return a.res.WriteData(name, data) }

// TestMoveKeyToDataRootKeepsOnionKey: a tor.key that an earlier version wrote
// to the config root is the key getPrivateKey returns after the move, so the
// onion address stays; the config copy is gone.
func TestMoveKeyToDataRootKeepsOnionKey(t *testing.T) {
	cfg, data := filepath.Join(t.TempDir(), "config"), filepath.Join(t.TempDir(), "data")
	res, err := resources.NewFileResources(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	res.SetDataRoot(data)
	stored := Key{4, 5, 6}
	if err := os.WriteFile(filepath.Join(cfg, privateKeyFileName), stored, 0600); err != nil {
		t.Fatal(err)
	}

	if err := moveKeyToDataRoot(res, alog.New(nil)); err != nil {
		t.Fatalf("moveKeyToDataRoot: %v", err)
	}

	mod := &Module{assets: dataAssets{res: res}}
	key, err := mod.getPrivateKey()
	if err != nil {
		t.Fatalf("getPrivateKey: %v", err)
	}
	if !bytes.Equal(key, stored) {
		t.Errorf("getPrivateKey = %x, want %x", key, stored)
	}
	if _, err := os.Stat(filepath.Join(cfg, privateKeyFileName)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("config copy: stat error = %v, want fs.ErrNotExist", err)
	}
}

// TestMoveKeyToDataRootMemResources pins that memory resources move nothing.
func TestMoveKeyToDataRootMemResources(t *testing.T) {
	if err := moveKeyToDataRoot(resources.NewMemResources(), alog.New(nil)); err != nil {
		t.Fatalf("moveKeyToDataRoot(mem) = %v, want nil", err)
	}
}
