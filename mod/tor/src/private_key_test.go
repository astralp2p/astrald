package tor

import (
	"bytes"
	"errors"
	"io/fs"
	"testing"

	"github.com/astralp2p/astrald/core/assets"
)

// keyAssets serves tor.key reads from a fixed result and records writes.
//
// why: the embedded nil interface satisfies assets.Assets without implementing
// it, so any other method panics. The Module has no torServer, so reaching
// generatePrivateKey panics too: both are assertions that getPrivateKey only
// reads.
type keyAssets struct {
	assets.Assets
	data   []byte
	err    error
	writes int
}

func (a *keyAssets) Read(string) ([]byte, error) { return a.data, a.err }

func (a *keyAssets) Write(string, []byte) error {
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
