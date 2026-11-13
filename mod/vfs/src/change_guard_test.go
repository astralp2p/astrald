package vfs

import (
	"errors"
	"testing"
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

// writeOpsCase is one change op routed by a caller who is not the VFS identity.
type writeOpsCase struct {
	fn   func(mod *Module) any
	args map[string]any
}

var writeOpsCases = map[string]writeOpsCase{
	"mkdir":  {func(mod *Module) any { return mod.OpMkdir }, map[string]any{"path": "/n"}},
	"touch":  {func(mod *Module) any { return mod.OpTouch }, map[string]any{"path": "/f"}},
	"remove": {func(mod *Module) any { return mod.OpRemove }, map[string]any{"path": "/f"}},
	"move":   {func(mod *Module) any { return mod.OpMove }, map[string]any{"from": "/f", "to": "/g"}},
}

// TestWriteOpsReadPermitDoesNotAllowChanging: a ReadAction permit refuses every change op and changes nothing; a
// WriteAction permit allows it.
func TestWriteOpsReadPermitDoesNotAllowChanging(t *testing.T) {
	for name, tc := range writeOpsCases {
		mod, vol, _ := changeTestModule(t)
		auth := newFakeAuth()
		mod.Auth = auth
		addFile(t, mod, vol, vol.RootID, "f", "x")
		changeTestSetTime(t, mod, vol.RootID, testModTime)
		caller := astral.GenerateIdentity()
		args := map[string]any{"vfs": vol.Identity.String()}
		for k, v := range tc.args {
			args[k] = v
		}

		auth.permit(&vfsapi.ReadAction{}, caller, vol.Identity)
		changeTestErr(t, changeTestRoute(t, tc.fn(mod), caller, args).receive(), vfsapi.ErrNotPermitted)

		f := changeTestFind(t, mod, vol, "/f")
		root := changeTestMustFind(t, mod, vol, "/")
		if n := changeTestCount(t, mod, vol); n != 1 || f == nil || f.ModTime != testModTime || root.ModTime != testModTime {
			t.Errorf("%s: a refused change changed the VFS", name)
		}

		auth.permit(&vfsapi.WriteAction{}, caller, vol.Identity)
		if answer := changeTestRoute(t, tc.fn(mod), caller, args).receive(); answer == nil {
			t.Errorf("%s: a write permit answered nothing", name)
		} else if e, ok := answer.(astral.Error); ok {
			t.Errorf("%s: a write permit answered %v", name, e)
		}
	}
}

// TestLimitsRefuseGrowthOnly: limits below the usage leave a change that does not grow the VFS alone, and refuse one
// that grows it.
func TestLimitsRefuseGrowthOnly(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	addFolder(t, mod, vol, vol.RootID, "d")
	x := addFolder(t, mod, vol, vol.RootID, "x")
	addFile(t, mod, vol, x.ID, "f", "12345")
	changeTestLowerLimits(t, mod, vol, 1, 1)

	err := mod.changeVFS(vol, func(c *change) (err error) {
		_, err = c.moveEntry([]string{"x", "f"}, []string{"d", "f"})
		return err
	})
	if err != nil {
		t.Fatalf("a move into an existing folder = %v, want no error", err)
	}
	if _, err = mod.recordFile(vol, []string{"d", "f"}, contentID("abcde")); err != nil {
		t.Fatalf("a replace of the same size = %v, want no error", err)
	}

	if _, err = mod.recordFile(vol, []string{"d", "g"}, contentID("z")); !errors.Is(err, vfsapi.ErrQuotaExceeded) {
		t.Errorf("a new file = %v, want %v", err, vfsapi.ErrQuotaExceeded)
	}
	err = mod.changeVFS(vol, func(c *change) error { return c.createFolder([]string{"e"}) })
	if !errors.Is(err, vfsapi.ErrTooManyEntries) {
		t.Errorf("a new folder = %v, want %v", err, vfsapi.ErrTooManyEntries)
	}
}

// TestLimitsCheckTheQuotaBeforeTheEntries: a change that breaks both limits answers vfsapi.ErrQuotaExceeded.
func TestLimitsCheckTheQuotaBeforeTheEntries(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	addFile(t, mod, vol, vol.RootID, "a", "x")
	changeTestLowerLimits(t, mod, vol, 1, 1)

	if _, err := mod.recordFile(vol, []string{"g"}, contentID("yy")); !errors.Is(err, vfsapi.ErrQuotaExceeded) {
		t.Fatalf("recordFile = %v, want %v", err, vfsapi.ErrQuotaExceeded)
	}
}

// TestFolderObjectIDAnswersACachedIDWithoutTheLock: a cached folder ID is answered while the lock of the VFS is held.
func TestFolderObjectIDAnswersACachedIDWithoutTheLock(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	addFile(t, mod, vol, vol.RootID, "f", "x")
	want, err := mod.folderObjectID(vol.Identity, vol.RootID)
	if err != nil {
		t.Fatal(err)
	}

	unlock := mod.lockVFS(vol.Identity)
	defer unlock()

	type answer struct {
		id  astral.ObjectID
		err error
	}
	got := make(chan answer, 1)
	go func() {
		id, err := mod.folderObjectID(vol.Identity, vol.RootID)
		got <- answer{id, err}
	}()

	select {
	case a := <-got:
		if a.err != nil || a.id != want {
			t.Errorf("folderObjectID = %v, %v; want %v", a.id, a.err, want)
		}
	case <-time.After(changeTestWait):
		t.Fatal("folderObjectID waited for the lock")
	}
}
