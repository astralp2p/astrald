package user

import (
	"errors"
	"testing"

	"github.com/cryptopunkscc/astrald/astral"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/mod/user"
)

// banModule builds a Module backed by an in-memory ban store, with issuer set as
// the active contract's issuer so IssueMembership's expelled guard can run.
func banModule(t *testing.T, issuer *astral.Identity) *Module {
	t.Helper()
	mod := &Module{db: testDB(t)}
	// seed the active-contract store directly; the tree.Value is unbound in tests,
	// so Set updates its local cache and ActiveContract() reads it back.
	err := mod.config.ActiveContract.Set(nil, &auth.SignedContract{Contract: &auth.Contract{Issuer: issuer}})
	if err != nil {
		t.Fatalf("seed active contract: %v", err)
	}
	return mod
}

// TestModule_isExpelled covers the guard's decision: a subject reads as expelled
// only for the issuer that banned it.
func TestModule_isExpelled(t *testing.T) {
	issuer := astral.GenerateIdentity()
	banned := astral.GenerateIdentity()
	allowed := astral.GenerateIdentity()

	mod := banModule(t, issuer)
	if err := mod.db.StoreExpulsion(sampleSigned(issuer, banned)); err != nil {
		t.Fatalf("store: %v", err)
	}

	if !mod.isExpelled(issuer, banned) {
		t.Error("banned subject not reported as expelled")
	}
	if mod.isExpelled(issuer, allowed) {
		t.Error("un-banned subject reported as expelled")
	}
	// a ban is scoped to its issuer; another issuer's swarm is unaffected
	if mod.isExpelled(astral.GenerateIdentity(), banned) {
		t.Error("ban leaked across issuers")
	}
}

// TestIssueMembership_RefusesExpelled proves the guard refuses a banned subject
// with user.ErrExpelled before any contract is minted — covering both OpAdopt and
// OpRequestMembership, which issue through this single chokepoint.
func TestIssueMembership_RefusesExpelled(t *testing.T) {
	issuer := astral.GenerateIdentity()
	banned := astral.GenerateIdentity()

	mod := banModule(t, issuer)
	if err := mod.db.StoreExpulsion(sampleSigned(issuer, banned)); err != nil {
		t.Fatalf("store: %v", err)
	}

	// nil ctx is safe: the guard returns before ctx is read.
	_, err := mod.IssueMembership(nil, banned)
	if !errors.Is(err, user.ErrExpelled) {
		t.Fatalf("expected ErrExpelled, got %v", err)
	}
}
