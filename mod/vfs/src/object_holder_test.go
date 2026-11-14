package vfs

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/objects/mem"
)

func TestHoldObjectHoldsTheFileOfAnyVFS(t *testing.T) {
	mod := testModule(t)
	a, b := createVFS(t, mod, astral.GenerateIdentity()), createVFS(t, mod, astral.GenerateIdentity())
	folder := addFolder(t, mod, a, a.RootID, "d")
	addFile(t, mod, a, folder.ID, "f", "in a")
	addFile(t, mod, b, b.RootID, "g", "in b")

	for name, content := range map[string]string{"first VFS": "in a", "second VFS": "in b"} {
		id := contentID(content)
		if !mod.HoldObject(&id) {
			t.Errorf("%s: not held", name)
		}
	}

	unknown := contentID("nobody has this")
	if mod.HoldObject(&unknown) {
		t.Error("an unknown ObjectID is held")
	}
}

// A file whose entry is gone is not held: removing the entry ends the hold.
func TestHoldObjectEndsWithTheEntry(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	file := addFile(t, mod, vol, vol.RootID, "f", "content")
	id := contentID("content")

	if !mod.HoldObject(&id) {
		t.Fatal("the file is not held")
	}
	if err := mod.db.Delete(file).Error; err != nil {
		t.Fatalf("delete: %v", err)
	}
	if mod.HoldObject(&id) {
		t.Error("an object stays held after its entry was removed")
	}
}

// The cached ObjectID of a folder is its current directory and is held until the folder changes.
func TestHoldObjectHoldsTheCurrentFolderID(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	folder := addFolder(t, mod, vol, vol.RootID, "d")

	root, err := mod.folderObjectID(vol.Identity, vol.RootID)
	if err != nil {
		t.Fatalf("folderObjectID: %v", err)
	}
	sub := cachedID(t, mod, folder.ID)
	if sub == nil {
		t.Fatal("the folder ID is not cached")
	}

	if !mod.HoldObject(&root) || !mod.HoldObject(sub) {
		t.Fatal("a current directory is not held")
	}

	if err = mod.db.clearFolderIDs(folder.ID, 0); err != nil {
		t.Fatalf("clearFolderIDs: %v", err)
	}
	if mod.HoldObject(&root) || mod.HoldObject(sub) {
		t.Error("a replaced directory is held")
	}
}

// A database error answers true, so that a failing lookup never lets an object be purged.
func TestHoldObjectHoldsOnDatabaseError(t *testing.T) {
	mod := testModule(t)
	createVFS(t, mod, astral.GenerateIdentity())

	if err := mod.db.Migrator().DropTable(&dbEntry{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	id := contentID("anything")
	if !mod.HoldObject(&id) {
		t.Error("a database error answered false")
	}
}

// The module is a holder for the objects module, which finds holders by asking each module that implements
// the interface.
func TestModuleIsAnObjectsHolder(t *testing.T) {
	var m any = testModule(t)

	if _, ok := m.(objectsmod.Holder); !ok {
		t.Fatal("the module does not implement objects.Holder")
	}
}

// An object held by a VFS is deleted from its repository as any other. The delete never asks a holder, and the
// VFS keeps its entry: ls and stat answer from the entries, which the repository does not touch.
func TestObjectHeldByAVFSCanBeDeletedAndTheEntryStaysVisible(t *testing.T) {
	mod := testModule(t)
	vol := createVFS(t, mod, astral.GenerateIdentity())
	file := addFile(t, mod, vol, vol.RootID, "f", "content")
	id := contentID("content")

	ctx := testContext()
	repo := mem.New("test", 0)

	w, err := repo.Create(ctx, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err = w.Write([]byte("content")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err = w.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if !mod.HoldObject(&id) {
		t.Fatal("the object is not held")
	}

	// this is the call of objects.delete
	if err = repo.Delete(ctx, &id); err != nil {
		t.Fatalf("delete of a held object: %v", err)
	}
	if ok, _ := repo.Contains(ctx, &id); ok {
		t.Fatal("the object is still in the repository")
	}

	// the lookup of stat
	found, err := mod.db.resolveEntry(vol, []string{"f"})
	if err != nil {
		t.Fatalf("resolve after the delete: %v", err)
	}
	if got := found.toEntry(); got.ID != id || got.Name != "f" || found.ID != file.ID {
		t.Errorf("entry after the delete = %+v, want the file %v", got, id)
	}

	// the lookup of ls
	children, err := mod.db.listChildren(vol.RootID)
	if err != nil || len(children) != 1 || children[0].Name != "f" || *children[0].ObjectID != id {
		t.Errorf("children after the delete = %v, %v; want the one file", children, err)
	}

	// the entry still holds the ObjectID
	if !mod.HoldObject(&id) {
		t.Error("the entry no longer holds the ObjectID")
	}
}
