package nearby

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/nearby"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// claimedUser is a user module whose local user is id.
type claimedUser struct {
	usermod.Module
	id *astral.Identity
}

func (u claimedUser) Identity() *astral.Identity { return u.id }

func TestResolveStatusStealthHintMaskedIDLength(t *testing.T) {
	userID := astral.GenerateIdentity()
	nodeID := astral.GenerateIdentity()
	masked := nearby.MaskIdentity(nodeID, userID)

	tests := []struct {
		name   string
		masked []astral.Uint8
		want   *astral.Identity
	}{
		{"valid", masked, nodeID},
		{"short", masked[:3], nil},
		{"empty", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := &Module{Deps: Deps{User: claimedUser{id: userID}}}

			var nonce astral.Nonce = 42
			status := &nearby.StatusMessage{Attachments: astral.NewBundle()}
			if err := status.Attachments.Append(&nearby.StealthHint{
				Commitment: nearby.ComputeCommitment(userID, nonce),
				MaskedID:   tt.masked,
				Nonce:      nonce,
			}); err != nil {
				t.Fatalf("Append: %v", err)
			}

			got := mod.ResolveStatus(status)
			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("ResolveStatus = %v; want nil", got)
			case tt.want != nil && (got == nil || !got.IsEqual(tt.want)):
				t.Fatalf("ResolveStatus = %v; want %v", got, tt.want)
			}
		})
	}
}
