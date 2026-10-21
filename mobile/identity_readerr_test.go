package mobile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/astralp2p/astrald/resources"
)

var errReadFailed = errors.New("read failed")

// failingResources fails every ReadData with errReadFailed and records WriteData calls.
//
// why: the embedded nil interface panics on Read and Write, which asserts that
// loadNodeIdentity never touches the config root.
type failingResources struct {
	resources.Resources
	writes int
}

func (r *failingResources) ReadData(string) ([]byte, error) { return nil, errReadFailed }

func (r *failingResources) WriteData(string, []byte) error {
	r.writes++
	return nil
}

// TestLoadNodeIdentityReadErrorKeepsKey: a node_key read error other than
// resources.ErrNotFound must be returned without generating or writing a key.
func TestLoadNodeIdentityReadErrorKeepsKey(t *testing.T) {
	res := &failingResources{}

	id, err := loadNodeIdentity(res)
	if !errors.Is(err, errReadFailed) {
		t.Fatalf("loadNodeIdentity = (%v, %v), want errReadFailed", id, err)
	}
	if res.writes != 0 {
		t.Fatalf("loadNodeIdentity wrote %d times, want 0", res.writes)
	}
}

// TestLoadNodeIdentityMissingKeyGenerates pins first-run key generation on a
// FileResources with no node_key: the key lands in the data root only.
func TestLoadNodeIdentityMissingKeyGenerates(t *testing.T) {
	cfg, data := filepath.Join(t.TempDir(), "config"), filepath.Join(t.TempDir(), "data")
	res, err := resources.NewFileResources(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	res.SetDataRoot(data)

	id1, err := loadNodeIdentity(res)
	if err != nil {
		t.Fatalf("first loadNodeIdentity: %v", err)
	}
	fi, err := os.Stat(filepath.Join(data, "node_key"))
	if err != nil {
		t.Fatalf("node_key not written to the data root: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("node_key mode = %o, want 600", got)
	}
	if _, err := os.Stat(filepath.Join(cfg, "node_key")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("node_key in the config root: stat error = %v, want fs.ErrNotExist", err)
	}

	id2, err := loadNodeIdentity(res)
	if err != nil {
		t.Fatalf("second loadNodeIdentity: %v", err)
	}
	if !id1.IsEqual(id2) {
		t.Fatalf("identity changed across loads: %v != %v", id1, id2)
	}
}
