package user

import (
	"errors"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	authsrc "github.com/astralp2p/astrald/mod/auth/src"
	cryptosrc "github.com/astralp2p/astrald/mod/crypto/src"
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

// TestIssueMembership_UnheldIssuerKey: an issuer key the node does not hold
// fails with one "sign as issuer: " prefix, not two.
func TestIssueMembership_UnheldIssuerKey(t *testing.T) {
	mod := banModule(t, astral.GenerateIdentity())
	mod.Auth = &authsrc.Module{Deps: authsrc.Deps{Crypto: &cryptosrc.Module{}}}

	_, err := mod.IssueMembership(astral.NewContext(nil), astral.GenerateIdentity())
	if want := "sign as issuer: no signing scheme available for key "; !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("IssueMembership err = %q; want prefix %q", err, want)
	}
}
