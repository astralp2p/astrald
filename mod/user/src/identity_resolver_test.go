package user

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// claimedModule builds a Module whose active contract names issuer as the user.
// A nil issuer leaves the module unclaimed. The tree.Value is unbound in tests,
// so Set updates its local cache and ActiveContract() reads it back.
func claimedModule(t *testing.T, issuer *astral.Identity) *Module {
	t.Helper()

	mod := &Module{}
	if issuer == nil {
		return mod
	}

	err := mod.config.ActiveContract.Set(nil, &auth.SignedContract{
		Contract: &auth.Contract{Issuer: issuer},
	})
	if err != nil {
		t.Fatalf("seed active contract: %v", err)
	}

	return mod
}

// TestResolveLocalUser covers the one name the resolver owns.
func TestResolveLocalUser(t *testing.T) {
	issuer := astral.GenerateIdentity()
	mod := claimedModule(t, issuer)

	got, err := mod.ResolveIdentity(LocalUser)
	if err != nil {
		t.Fatalf("resolve %v: %v", LocalUser, err)
	}
	if !got.IsEqual(issuer) {
		t.Fatalf("resolved %v, expected the active contract's issuer %v", got, issuer)
	}
}

// TestResolveLocalUserUnclaimed proves an unclaimed node errors rather than
// resolving the name to a nil identity, which dir would take as a hit.
func TestResolveLocalUserUnclaimed(t *testing.T) {
	mod := claimedModule(t, nil)

	got, err := mod.ResolveIdentity(LocalUser)
	if !errors.Is(err, user.ErrNoActiveContract) {
		t.Fatalf("expected ErrNoActiveContract, got %v", err)
	}
	if got != nil {
		t.Fatalf("resolved %v, expected no identity", got)
	}
}

// TestResolveLeavesOtherNames proves the resolver declines everything else, so
// dir's alias table and the resolvers below keep their names.
func TestResolveLeavesOtherNames(t *testing.T) {
	mod := claimedModule(t, astral.GenerateIdentity())

	for _, name := range []string{"", "localnode", "anyone", "alice", "LocalUser", "localuser2"} {
		got, err := mod.ResolveIdentity(name)
		if err == nil {
			t.Errorf("resolved %q to %v, expected the name to fall through", name, got)
		}
	}
}

// TestDisplayNameIsEmpty proves the resolver never shadows the alias table or
// dir's fingerprint fallback.
func TestDisplayNameIsEmpty(t *testing.T) {
	issuer := astral.GenerateIdentity()
	mod := claimedModule(t, issuer)

	if got := mod.DisplayName(issuer); got != "" {
		t.Fatalf("display name %q, expected empty", got)
	}
}
