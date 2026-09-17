package nearby

import (
	"sync/atomic"
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

type fixedUser struct {
	usermod.Module
	id    *astral.Identity
	calls atomic.Uint64
}

func (u *fixedUser) Identity() *astral.Identity {
	u.calls.Add(1)
	return u.id
}

func statusWith(t *testing.T, attachments ...astral.Object) *nearby.StatusMessage {
	t.Helper()

	bundle := astral.NewBundle()
	if err := bundle.Append(attachments...); err != nil {
		t.Fatalf("Append: %v", err)
	}
	return &nearby.StatusMessage{Attachments: bundle}
}

func stealthHint(node, user *astral.Identity, nonce astral.Nonce) *nearby.StealthHint {
	return &nearby.StealthHint{
		Commitment: nearby.ComputeCommitment(user, nonce),
		MaskedID:   nearby.MaskIdentity(node, user),
		Nonce:      nonce,
	}
}

func TestResolveStatusPublicProfileSkipsUser(t *testing.T) {
	n := astral.GenerateIdentity()
	user := &fixedUser{id: astral.GenerateIdentity()}
	mod := &Module{Deps: Deps{User: user}}

	got := mod.ResolveStatus(statusWith(t, &nearby.PublicProfile{NodeID: n}))
	if !got.IsEqual(n) {
		t.Errorf("ResolveStatus = %v; want %v", got, n)
	}
	if c := user.calls.Load(); c != 0 {
		t.Errorf("User.Identity called %d times; want 0", c)
	}
}

func TestResolveStatusStealthHint(t *testing.T) {
	n, u, other := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()

	mismatchedNonce := stealthHint(n, u, 9)
	mismatchedNonce.Nonce = 10

	tests := []struct {
		name string
		user *astral.Identity
		hint *nearby.StealthHint
		want *astral.Identity
	}{
		{"owner user unmasks node", u, stealthHint(n, u, 9), n},
		{"no user", nil, stealthHint(n, u, 9), nil},
		{"other user fails commitment", other, stealthHint(n, u, 9), nil},
		{"nonce does not match commitment", u, mismatchedNonce, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod := &Module{Deps: Deps{User: &fixedUser{id: tt.user}}}

			got := mod.ResolveStatus(statusWith(t, tt.hint))
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("ResolveStatus = %v; want nil", got)
			case tt.want != nil && !got.IsEqual(tt.want):
				t.Errorf("ResolveStatus = %v; want %v", got, tt.want)
			}
		})
	}
}

func TestResolveStatusPrefersPublicProfile(t *testing.T) {
	a, b, u := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := &Module{Deps: Deps{User: &fixedUser{id: u}}}

	got := mod.ResolveStatus(statusWith(t, &nearby.PublicProfile{NodeID: a}, stealthHint(b, u, 9)))
	if !got.IsEqual(a) {
		t.Errorf("ResolveStatus = %v; want profile node %v", got, a)
	}
}

func TestResolveStatusEmptyBundle(t *testing.T) {
	mod := &Module{Deps: Deps{User: &fixedUser{id: astral.GenerateIdentity()}}}

	if got := mod.ResolveStatus(statusWith(t)); got != nil {
		t.Errorf("ResolveStatus(empty) = %v; want nil", got)
	}
}
