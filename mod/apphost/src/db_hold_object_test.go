package apphost

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// TestHoldObjectReHoldMerges: a re-hold of an already-held object updates the
// existing hold. A permanent hold always wins; between two expiring holds the
// later expiry wins; a shorter re-hold never shortens an existing hold.
func TestHoldObjectReHoldMerges(t *testing.T) {
	dur := func(d time.Duration) *astral.Duration { v := astral.Duration(d); return &v }

	tests := []struct {
		name          string
		first, second *astral.Duration // nil is a permanent hold
		want          string           // "permanent", "first" or "second"
	}{
		{name: "permanent over expiring", first: dur(time.Hour), second: nil, want: "permanent"},
		{name: "expiring over permanent", first: nil, second: dur(time.Hour), want: "permanent"},
		{name: "later over earlier", first: dur(time.Hour), second: dur(2 * time.Hour), want: "second"},
		{name: "earlier over later", first: dur(2 * time.Hour), second: dur(time.Hour), want: "first"},
		{name: "renewal of a lapsed hold", first: dur(-time.Hour), second: dur(time.Hour), want: "second"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := holdModule(t).db
			app, obj := astral.GenerateIdentity(), heldFullID()

			holdUntil := func() *time.Time {
				t.Helper()
				var row dbObjectHold
				if err := db.Where("app_id = ? AND object_id = ?", app, obj).First(&row).Error; err != nil {
					t.Fatalf("read hold: %v", err)
				}
				return row.HoldUntil
			}

			if err := db.HoldObject(app, obj, tt.first); err != nil {
				t.Fatalf("first hold: %v", err)
			}
			first := holdUntil()
			if err := db.HoldObject(app, obj, tt.second); err != nil {
				t.Fatalf("second hold: %v", err)
			}
			got := holdUntil()

			switch tt.want {
			case "permanent":
				if got != nil {
					t.Fatalf("hold_until = %v, want permanent (NULL)", got)
				}
			case "first":
				if got == nil || !got.Equal(*first) {
					t.Fatalf("hold_until = %v, want unchanged %v", got, first)
				}
			case "second":
				if got == nil || !got.After(*first) || got.Before(time.Now().Add(time.Duration(*tt.second)-time.Minute)) {
					t.Fatalf("hold_until = %v, want the second hold's expiry (~now+%v), first was %v", got, time.Duration(*tt.second), first)
				}
			}

			held, err := db.ObjectHeld(obj)
			if err != nil {
				t.Fatalf("ObjectHeld: %v", err)
			}
			if !held {
				t.Fatal("object not held after re-hold")
			}
		})
	}
}
