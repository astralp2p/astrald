package objects

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// TestRepoGroup_EmptyObjectWithoutMembers: a group with no members answers the empty object without storage.
func TestRepoGroup_EmptyObjectWithoutMembers(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		group := addGroup(t, &Module{}, "g", concurrent)
		ctx := astral.NewContext(nil)
		empty := objectsmod.EmptyObjectID()

		r, err := group.Read(ctx, empty, 0, 0)
		if err != nil {
			t.Fatalf("concurrent=%v: Read: %v", concurrent, err)
		}
		if got := r.ID(); !got.IsEqual(empty) {
			t.Errorf("concurrent=%v: ID() = %v, want %v", concurrent, got, empty)
		}
		if r.Repo() != objectsmod.Repository(group) {
			t.Errorf("concurrent=%v: Repo() = %v, want the group", concurrent, r.Repo())
		}
		if data := readString(t, r); data != "" {
			t.Errorf("concurrent=%v: Read = %q, want no bytes", concurrent, data)
		}
		r.Close()

		if _, err := group.Read(ctx, empty, 1, 0); !errors.Is(err, objectsmod.ErrOutOfBounds) {
			t.Errorf("concurrent=%v: Read at offset 1 = %v, want ErrOutOfBounds", concurrent, err)
		}
		if ok, err := group.Contains(ctx, empty); !ok || err != nil {
			t.Errorf("concurrent=%v: Contains = %v, %v, want true, nil", concurrent, ok, err)
		}
		if err := group.Delete(ctx, empty); err != nil {
			t.Errorf("concurrent=%v: Delete = %v, want nil", concurrent, err)
		}
	}
}

// TestRepoGroup_EmptyObjectReadsThroughMembers: a group with members reads the empty object from a member,
// so the member's wrapper sees the read; a commit through the group stores nothing in any member.
func TestRepoGroup_EmptyObjectReadsThroughMembers(t *testing.T) {
	_, group, m1, m2 := newTestGroup(t, false)
	ctx := astral.NewContext(nil)

	id := commitTo(t, group, "")
	if !id.IsEqual(objectsmod.EmptyObjectID()) {
		t.Fatalf("commit = %v, want the empty ObjectID", id)
	}
	if m1.Used() != 0 || m2.Used() != 0 {
		t.Fatalf("members use %v and %v bytes, want none", m1.Used(), m2.Used())
	}

	r, err := group.Read(ctx, id, 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	defer r.Close()
	if r.Repo() != objectsmod.Repository(m1) {
		t.Errorf("Repo() = %v, want the first member", r.Repo())
	}
}

// TestRepoGroup_PartialSizeZeroIDStaysPartial: a size-0 ID of another hash reaches the members as a partial ID.
func TestRepoGroup_PartialSizeZeroIDStaysPartial(t *testing.T) {
	_, group, _, m2 := newTestGroup(t, false)
	ctx := astral.NewContext(nil)
	full := commitTo(t, m2, "hello")
	partial := &astral.ObjectID{Hash: full.Hash}

	r, err := group.Read(ctx, partial, 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	defer r.Close()
	if got := r.ID(); !got.IsEqual(full) {
		t.Errorf("ID() = %v, want %v", got, full)
	}
	if data := readString(t, r); data != "hello" {
		t.Errorf("Read = %q, want hello", data)
	}

	unknown := unknownObjectID(t)
	unknown.Size = 0
	if ok, err := group.Contains(ctx, unknown); ok || err != nil {
		t.Errorf("Contains(unknown partial) = %v, %v, want false, nil", ok, err)
	}
	if _, err := group.Read(ctx, unknown, 0, 0); !errors.Is(err, objectsmod.ErrNotFound) {
		t.Errorf("Read(unknown partial) = %v, want ErrNotFound", err)
	}
	if err := group.Delete(ctx, unknown); !errors.Is(err, objectsmod.ErrNotFound) {
		t.Errorf("Delete(unknown partial) = %v, want ErrNotFound", err)
	}
}

// TestTracking_EmptyObjectDeleteSucceeds: a tracked leaf deletes the empty object without error, so purge never fails on it.
func TestTracking_EmptyObjectDeleteSucceeds(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	repo := mod.GetRepository("mem0")

	id := commitData(t, ctx, repo, "")
	if !id.IsEqual(objectsmod.EmptyObjectID()) {
		t.Fatalf("commit = %v, want the empty ObjectID", id)
	}

	if err := repo.Delete(ctx, id); err != nil {
		t.Fatalf("Delete = %v, want nil", err)
	}
	if ok, err := repo.Contains(ctx, id); !ok || err != nil {
		t.Fatalf("Contains after Delete = %v, %v, want true, nil", ok, err)
	}
}
