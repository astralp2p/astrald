package vfs

import (
	"errors"
	"testing"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
)

// changeTestLowerLimits commits new limits through vfs.set_quota after the caller read the VFS, as a set_quota that
// runs between the authorization of a change and the change does.
func changeTestLowerLimits(t *testing.T, mod *Module, vol *dbVolume, bytes, entries uint64) {
	t.Helper()

	if err := mod.setLimits(vol.Identity, bytes, entries); err != nil {
		t.Fatalf("setLimits: %v", err)
	}
}

func TestWriteCommitChecksTheLimitsCommittedAtTheChange(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	changeTestLowerLimits(t, mod, vol, 3, 0)

	_, err := mod.writeCommit(vol, []string{"f"}, contentID("12345"))
	if !errors.Is(err, vfsapi.ErrQuotaExceeded) {
		t.Fatalf("writeCommit = %v, want %v", err, vfsapi.ErrQuotaExceeded)
	}
	if n := changeTestCount(t, mod, vol); n != 0 {
		t.Errorf("%d entries after a refused write, want 0", n)
	}
}

func TestMkdirChecksTheLimitsCommittedAtTheChange(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	changeTestLowerLimits(t, mod, vol, 0, 1)

	err := mod.changeVFS(vol, func(c *change) error { return c.createFolder([]string{"a", "b"}) })
	if !errors.Is(err, vfsapi.ErrTooManyEntries) {
		t.Fatalf("mkdir = %v, want %v", err, vfsapi.ErrTooManyEntries)
	}
	if n := changeTestCount(t, mod, vol); n != 0 {
		t.Errorf("%d entries after a refused mkdir, want 0", n)
	}
}

func TestMoveChecksTheLimitsCommittedAtTheChange(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	addFile(t, mod, vol, vol.RootID, "f", "x")
	changeTestLowerLimits(t, mod, vol, 0, 1)

	err := mod.changeVFS(vol, func(c *change) (err error) {
		_, err = c.moveEntry([]string{"f"}, []string{"new", "f"})
		return err
	})
	if !errors.Is(err, vfsapi.ErrTooManyEntries) {
		t.Fatalf("move = %v, want %v", err, vfsapi.ErrTooManyEntries)
	}
	if changeTestFind(t, mod, vol, "/f") == nil || changeTestCount(t, mod, vol) != 1 {
		t.Error("a refused move changed the VFS")
	}
}
