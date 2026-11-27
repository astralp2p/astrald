package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// banModule builds a Module over an in-memory SwarmState, with issuer set as
// the active contract's issuer.
func banModule(t testing.TB, issuer *astral.Identity) *Module {
	t.Helper()
	mod := &Module{db: testDB(t)}
	mod.db.mod = mod
	// seed the active-contract store directly; the tree.Value is unbound in tests,
	// so Set updates its local cache and ActiveContract() reads it back.
	err := mod.config.ActiveContract.Set(nil, &auth.SignedContract{Contract: &auth.Contract{Issuer: issuer}})
	if err != nil {
		t.Fatalf("seed active contract: %v", err)
	}
	useSwarm(t, mod, issuer)
	return mod
}

// TestExpelledReadsTheStoredBans covers the guard's decision: a subject reads as
// expelled once an Expulsion names it.
func TestExpelledReadsTheStoredBans(t *testing.T) {
	issuer := astral.GenerateIdentity()
	banned := astral.GenerateIdentity()
	allowed := astral.GenerateIdentity()

	mod := banModule(t, issuer)
	banNode(t, mod, issuer, banned)

	if !mod.Expelled(banned) {
		t.Error("banned subject not reported as expelled")
	}
	if mod.Expelled(allowed) {
		t.Error("un-banned subject reported as expelled")
	}
}
