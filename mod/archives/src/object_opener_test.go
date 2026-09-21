package archives

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

// TestOpenObjectReachesTheEntryLookup: an unknown object reports ErrNotFound,
// which it can only do by reaching the lookup. Before the fix the bounds guard
// fired for every object and returned ErrOutOfBounds instead.
func TestOpenObjectReachesTheEntryLookup(t *testing.T) {
	mod := newTestModule(t)
	ctx := astral.NewContext(nil).WithZone(astral.ZoneVirtual)

	r, err := mod.OpenObject(ctx, testObjectID(t, "absent"))

	if !errors.Is(err, objects.ErrNotFound) {
		t.Errorf("OpenObject err = %v; want ErrNotFound", err)
	}
	if r != nil {
		t.Errorf("OpenObject returned a reader for an object it does not hold: %v", r)
	}
}

// TestOpenObjectRefusesANonVirtualZone pins the guard the change sits beside.
//
// why an explicit zone: NewContext defaults to ZoneDefault, which already
// includes ZoneVirtual.
func TestOpenObjectRefusesANonVirtualZone(t *testing.T) {
	mod := newTestModule(t)
	ctx := astral.NewContext(nil).WithZone(astral.ZoneDevice)

	if _, err := mod.OpenObject(ctx, testObjectID(t, "absent")); !errors.Is(err, astral.ErrZoneExcluded) {
		t.Errorf("OpenObject err = %v; want ErrZoneExcluded", err)
	}
}
