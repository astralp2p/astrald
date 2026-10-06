package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/astralp2p/astrald/resources"
)

var errReadFailed = errors.New("read failed")

// failingResources fails every Read with errReadFailed and records Writes.
type failingResources struct {
	writes int
}

func (r *failingResources) Read(string) ([]byte, error) { return nil, errReadFailed }

func (r *failingResources) Write(string, []byte) error {
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
// FileResources root with no node_key.
func TestLoadNodeIdentityMissingKeyGenerates(t *testing.T) {
	root := t.TempDir()
	res, err := resources.NewFileResources(root, false)
	if err != nil {
		t.Fatal(err)
	}

	id1, err := loadNodeIdentity(res)
	if err != nil {
		t.Fatalf("first loadNodeIdentity: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "node_key")); err != nil {
		t.Fatalf("node_key not written: %v", err)
	}

	id2, err := loadNodeIdentity(res)
	if err != nil {
		t.Fatalf("second loadNodeIdentity: %v", err)
	}
	if !id1.IsEqual(id2) {
		t.Fatalf("identity changed across loads: %v != %v", id1, id2)
	}
}
