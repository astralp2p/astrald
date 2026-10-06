package nearby

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

func TestUnmaskIdentityRejectsWrongLength(t *testing.T) {
	userID := astral.GenerateIdentity()
	nodeID := astral.GenerateIdentity()
	valid := MaskIdentity(nodeID, userID)

	tests := []struct {
		name   string
		masked []astral.Uint8
	}{
		{"nil", nil},
		{"short", valid[:3]},
		{"one byte short", valid[:32]},
		{"one byte long", append(append([]astral.Uint8(nil), valid...), 0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := UnmaskIdentity(tt.masked, userID)
			if err == nil {
				t.Fatalf("UnmaskIdentity(len %d) = %v, nil; want error", len(tt.masked), id)
			}
			if id != nil {
				t.Fatalf("UnmaskIdentity(len %d) identity = %v; want nil", len(tt.masked), id)
			}
		})
	}
}

func TestUnmaskIdentityRoundTrip(t *testing.T) {
	userID := astral.GenerateIdentity()
	nodeID := astral.GenerateIdentity()

	id, err := UnmaskIdentity(MaskIdentity(nodeID, userID), userID)
	if err != nil {
		t.Fatalf("UnmaskIdentity: %v", err)
	}
	if !id.IsEqual(nodeID) {
		t.Fatalf("UnmaskIdentity = %v; want %v", id, nodeID)
	}
}
