package vfs

import (
	"errors"
	"testing"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

func TestCreateVFSDefaults(t *testing.T) {
	mod := testModule(t)
	id := astral.GenerateIdentity()

	vol := createVFS(t, mod, id)

	if vol.byteLimit() != 1<<30 {
		t.Errorf("byte limit = %d, want 1 GiB", vol.byteLimit())
	}
	if vol.entryLimit() != 1_000_000 {
		t.Errorf("entry limit = %d, want 1,000,000", vol.entryLimit())
	}
	if !vol.Identity.IsEqual(id) {
		t.Errorf("identity = %v, want %v", vol.Identity, id)
	}

	root, err := mod.db.getEntry(vol.RootID)
	if err != nil || !root.isRoot() || !root.VFS.IsEqual(id) {
		t.Fatalf("root = %+v, %v", root, err)
	}
}

func TestCreateVFSUsesConfiguredLimits(t *testing.T) {
	mod := testModule(t)
	mod.config = Config{ByteLimit: 7, EntryLimit: 3}

	vol := createVFS(t, mod, astral.GenerateIdentity())
	if vol.byteLimit() != 7 || vol.entryLimit() != 3 {
		t.Fatalf("limits = %d, %d, want 7, 3", vol.byteLimit(), vol.entryLimit())
	}

	mod.config = Config{} // 0 means no limit
	vol = createVFS(t, mod, astral.GenerateIdentity())
	if vol.byteLimit() != 0 || vol.entryLimit() != 0 {
		t.Fatalf("limits = %d, %d, want 0, 0", vol.byteLimit(), vol.entryLimit())
	}
}

func TestCreateVFSIsIdempotent(t *testing.T) {
	mod := testModule(t)
	id := astral.GenerateIdentity()

	first := createVFS(t, mod, id)
	file := addFile(t, mod, first, first.RootID, "keep", "x")

	// a changed default does not touch an existing VFS
	mod.config = Config{ByteLimit: 1, EntryLimit: 1}

	second := createVFS(t, mod, id)
	if second.ID != first.ID || second.RootID != first.RootID || second.ByteLimit != first.ByteLimit || second.EntryLimit != first.EntryLimit {
		t.Fatalf("second create changed the volume: %+v, was %+v", second, first)
	}
	if _, err := mod.db.getEntry(file.ID); err != nil {
		t.Fatalf("second create lost an entry: %v", err)
	}

	var volumes, roots int64
	mod.db.Model(&dbVolume{}).Count(&volumes)
	mod.db.Model(&dbEntry{}).Where("id = parent_id").Count(&roots)
	if volumes != 1 || roots != 1 {
		t.Fatalf("%d volumes and %d roots, want 1 and 1", volumes, roots)
	}
}

func TestCreateVFSRefusesTheZeroIdentity(t *testing.T) {
	mod := testModule(t)

	for _, id := range []*astral.Identity{nil, {}} {
		if err := mod.CreateVFS(id); !errors.Is(err, vfsapi.ErrNotPermitted) {
			t.Errorf("CreateVFS(%v) = %v, want not permitted", id, err)
		}
	}

	var n int64
	mod.db.Model(&dbVolume{}).Count(&n)
	if n != 0 {
		t.Fatalf("%d volumes after refused creates", n)
	}
}

func TestCreateVFSRefusesTheNode(t *testing.T) {
	mod := testModule(t)

	if err := mod.CreateVFS(mod.identity); !errors.Is(err, vfsapi.ErrNotPermitted) {
		t.Fatalf("CreateVFS(node) = %v, want not permitted", err)
	}
	if _, err := mod.db.findVolume(mod.identity); !errors.Is(err, vfsapi.ErrNoSuchVFS) {
		t.Fatalf("a VFS exists for the node: %v", err)
	}
}

func TestCreateVFSConcurrent(t *testing.T) {
	mod := testModule(t)
	id := astral.GenerateIdentity()

	errs := make(chan error, 8)
	for i := 0; i < cap(errs); i++ {
		go func() { errs <- mod.CreateVFS(id) }()
	}
	for i := 0; i < cap(errs); i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent CreateVFS: %v", err)
		}
	}

	var n int64
	mod.db.Model(&dbVolume{}).Count(&n)
	if n != 1 {
		t.Fatalf("%d volumes, want 1", n)
	}
}

func TestLimitColumnClamps(t *testing.T) {
	if got := limitColumn(1<<64 - 1); got != 1<<63-1 {
		t.Fatalf("limitColumn(max) = %d", got)
	}

	mod := testModule(t)
	if err := mod.db.createVolume(astral.GenerateIdentity(), 1<<64-1, 5); err != nil {
		t.Fatalf("createVolume with a huge limit: %v", err)
	}
}
