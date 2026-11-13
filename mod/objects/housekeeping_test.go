package objects

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// TestHousekeeping_MarkKeepsIdentityAndZone: the mark is set on a copy, and the copy keeps identity and zone.
func TestHousekeeping_MarkKeepsIdentityAndZone(t *testing.T) {
	id := astral.GenerateIdentity()
	ctx := astral.NewContext(nil).WithIdentity(id).WithZone(astral.ZoneDevice)

	marked := WithHousekeeping(ctx)

	if !IsHousekeeping(marked) {
		t.Fatal("the marked context is not housekeeping")
	}
	if IsHousekeeping(ctx) {
		t.Fatal("WithHousekeeping marked the original context")
	}
	if !marked.Identity().IsEqual(id) || marked.Zone() != astral.ZoneDevice {
		t.Fatalf("marked context has identity %v and zone %v; want %v and %v", marked.Identity(), marked.Zone(), id, astral.ZoneDevice)
	}

	child, cancel := marked.WithCancel()
	defer cancel()
	if !IsHousekeeping(child) {
		t.Fatal("a child of a marked context lost the mark")
	}
}
