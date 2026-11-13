package objects

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// TestDeleteUnheld_DeletesFromEveryLeaf: an unheld object committed before since is deleted from every leaf of a
// group, with its rows.
func TestDeleteUnheld_DeletesFromEveryLeaf(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	addLocalLeaf(t, mod, "disk1")
	addLocalLeaf(t, mod, "disk2")
	id := commitData(t, ctx, mod.GetRepository("disk1"), "old")
	commitData(t, ctx, mod.GetRepository("disk2"), "old")
	setTimes(t, mod, "disk1", id, ago(2*time.Hour), 0)
	setTimes(t, mod, "disk2", id, ago(2*time.Hour), 0)

	err := mod.DeleteUnheld(ctx, mod.GetRepository(objects.RepoLocal), id, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	expectContains(t, mod, "disk1", id, false)
	expectContains(t, mod, "disk2", id, false)
	if len(trackingRows(t, mod, "disk1"))+len(trackingRows(t, mod, "disk2")) != 0 {
		t.Fatal("a row of the deleted object is left")
	}
}

// TestDeleteUnheld_KeepsObjectCommittedAfterSince: a leaf that committed the object after since keeps it; a leaf
// that committed it before deletes it.
func TestDeleteUnheld_KeepsObjectCommittedAfterSince(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	addLocalLeaf(t, mod, "disk1")
	addLocalLeaf(t, mod, "disk2")
	id := commitData(t, ctx, mod.GetRepository("disk1"), "committed again")
	commitData(t, ctx, mod.GetRepository("disk2"), "committed again")
	setTimes(t, mod, "disk1", id, ago(2*time.Hour), 0)

	err := mod.DeleteUnheld(ctx, mod.GetRepository(objects.RepoLocal), id, time.Now().Add(-time.Hour))
	if !errors.Is(err, objectsmod.ErrObjectInUse) {
		t.Fatalf("delete: %v; want ErrObjectInUse", err)
	}
	expectContains(t, mod, "disk1", id, false)
	expectContains(t, mod, "disk2", id, true)
}

// TestDeleteUnheld_KeepsHeldObject: a held object is kept, whatever its commit time.
func TestDeleteUnheld_KeepsHeldObject(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	id := commitData(t, ctx, mod.GetRepository("mem0"), "held")
	setTimes(t, mod, "mem0", id, 1, 0)
	mod.AddHolder(holderFunc(func(h *astral.ObjectID) bool { return h.IsEqual(id) }))

	err := mod.DeleteUnheld(ctx, mod.GetRepository("mem0"), id, time.Now())
	if !errors.Is(err, objectsmod.ErrObjectInUse) {
		t.Fatalf("delete: %v; want ErrObjectInUse", err)
	}
	expectContains(t, mod, "mem0", id, true)
}

// TestDeleteUnheld_MissingIsNotFound: an object no leaf has answers ErrNotFound.
func TestDeleteUnheld_MissingIsNotFound(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	id := &astral.ObjectID{Size: 4, Hash: [32]byte{1}}

	err := mod.DeleteUnheld(ctx, mod.GetRepository("mem0"), id, time.Now())
	if !errors.Is(err, objectsmod.ErrNotFound) {
		t.Fatalf("delete: %v; want ErrNotFound", err)
	}
}

// TestDeleteUnheld_DecidesUnderTheLeafLock: the decision waits for a commit that holds the leaf's lock, so the
// commit's time keeps the object.
func TestDeleteUnheld_DecidesUnderTheLeafLock(t *testing.T) {
	mod := newTrackingModule(t)
	ctx := astral.NewContext(nil)
	leaf := mod.GetRepository("mem0").(*trackedRepository)
	id := commitData(t, ctx, leaf, "committed under the lock")
	setTimes(t, mod, "mem0", id, ago(2*time.Hour), 0)

	leaf.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- mod.DeleteUnheld(ctx, leaf, id, time.Now().Add(-time.Hour)) }()

	select {
	case <-done:
		leaf.mu.Unlock()
		t.Fatal("the delete decided while the leaf's lock was held")
	case <-time.After(100 * time.Millisecond):
	}
	// why: stands for a commit that holds the leaf's lock and writes the commit time.
	setTimes(t, mod, "mem0", id, time.Now().Unix(), 0)
	leaf.mu.Unlock()

	select {
	case err := <-done:
		if !errors.Is(err, objectsmod.ErrObjectInUse) {
			t.Fatalf("delete: %v; want ErrObjectInUse", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the delete did not decide after the lock was released")
	}
	expectContains(t, mod, "mem0", id, true)
}
