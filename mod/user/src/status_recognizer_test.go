package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
)

// statusOf returns a status that carries objs.
func statusOf(t *testing.T, objs ...astral.Object) *nearby.StatusMessage {
	t.Helper()

	bundle := astral.NewBundle()
	if err := bundle.Append(objs...); err != nil {
		t.Fatalf("bundle: %v", err)
	}
	return &nearby.StatusMessage{Attachments: bundle}
}

func stealthHint(t *testing.T, node, user *astral.Identity) *nearby.StealthHint {
	t.Helper()

	hint, err := nearby.NewStealthHint(node, user, astral.NewNonce())
	if err != nil {
		t.Fatalf("hint: %v", err)
	}
	return hint
}

// TestRecognizeStatus: a node of a user recognizes a hint that opens with the
// user and a profile that names the user and a node, and nothing else.
func TestRecognizeStatus(t *testing.T) {
	userID, peerID, otherUser := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := claimedModule(t, userID)

	malformed := stealthHint(t, peerID, userID)
	malformed.Commitment = malformed.Commitment[:31]

	cases := []struct {
		name   string
		status *nearby.StatusMessage
		want   bool
	}{
		{"hint of the user", statusOf(t, stealthHint(t, peerID, userID)), true},
		{"hint of another user", statusOf(t, stealthHint(t, peerID, otherUser)), false},
		{"malformed hint", statusOf(t, malformed), false},
		{"profile of the user", statusOf(t, &nearby.PublicProfile{NodeID: peerID, UserID: userID}), true},
		{"adoptable profile", statusOf(t, nearby.NewFlag("adoptable"), &nearby.PublicProfile{NodeID: peerID}), false},
		{"profile of another user", statusOf(t, &nearby.PublicProfile{NodeID: peerID, UserID: otherUser}), false},
		{"profile of the user with no node", statusOf(t, &nearby.PublicProfile{UserID: userID}), false},
		{"flag alone", statusOf(t, nearby.NewFlag("adoptable")), false},
		{"empty bundle", statusOf(t), false},
		{"no bundle", &nearby.StatusMessage{}, false},
		{"no status", nil, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := mod.RecognizeStatus(tt.status)
			if (rec != nil) != tt.want {
				t.Fatalf("recognized %+v; want recognized %v", rec, tt.want)
			}
			if rec == nil {
				return
			}
			if !rec.NodeID.IsEqual(peerID) || !rec.UserID.IsEqual(userID) {
				t.Fatalf("recognized %v of %v; want %v of %v", rec.NodeID, rec.UserID, peerID, userID)
			}
		})
	}
}

// TestRecognizeStatusWithoutAUser: a node without a user recognizes nothing,
// also a profile with no user.
func TestRecognizeStatusWithoutAUser(t *testing.T) {
	mod := claimedModule(t, nil)
	peerID := astral.GenerateIdentity()

	for _, status := range []*nearby.StatusMessage{
		statusOf(t, &nearby.PublicProfile{NodeID: peerID}),
		statusOf(t, &nearby.PublicProfile{NodeID: peerID, UserID: astral.GenerateIdentity()}),
	} {
		if rec := mod.RecognizeStatus(status); rec != nil {
			t.Fatal("a node without a user recognized a status")
		}
	}
}

// TestRecognizeAttachmentRefusesGarbage: zero identities, nil attachments and
// other objects are not recognized, and none panics.
func TestRecognizeAttachmentRefusesGarbage(t *testing.T) {
	userID, peerID := astral.GenerateIdentity(), astral.GenerateIdentity()
	zero := &astral.Identity{}

	for name, obj := range map[string]astral.Object{
		"zero user":       &nearby.PublicProfile{NodeID: peerID, UserID: zero},
		"zero node":       &nearby.PublicProfile{NodeID: zero, UserID: userID},
		"nil profile":     (*nearby.PublicProfile)(nil),
		"nil hint":        (*nearby.StealthHint)(nil),
		"empty hint":      &nearby.StealthHint{},
		"nil object":      nil,
		"another object":  &nearby.ScanMessage{},
		"zero both":       &nearby.PublicProfile{NodeID: zero, UserID: zero},
		"profile no user": &nearby.PublicProfile{NodeID: peerID},
	} {
		t.Run(name, func(t *testing.T) {
			if node, ok := recognizeAttachment(obj, userID); ok || node != nil {
				t.Fatalf("recognized %v (%v); want nothing", node, ok)
			}
		})
	}
}
