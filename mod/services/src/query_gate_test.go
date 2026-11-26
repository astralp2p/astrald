package services

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astrald/core"
)

// gate runs the query gate on a query from caller to target.
func gate(m *Module, caller, target *astral.Identity) error {
	q := astral.Launch(query.New(caller, target, "bitcoin-wallet.spend", nil))
	return m.PreprocessQuery(core.NewQueryModifier(q))
}

// Gate 1 for a query: an app reaches a provider of a private service only with
// a discovery permit for it on this node; an open provider, the provider
// itself and this node are not gated.
func TestQueryToAPrivateProviderNeedsADiscoveryPermit(t *testing.T) {
	v := newVisibilitySwarm()
	_, wallet := advertise(t, v.a, "bitcoin-wallet&open=false", available)
	_, player := advertise(t, v.a, "player", available)
	node := v.a.node.Identity()

	if err := gate(v.a, v.app, wallet); err == nil {
		t.Fatal("an app without a permit reached the private wallet")
	}
	if err := gate(v.a, v.app, player); err != nil {
		t.Fatalf("an open provider was gated: %v", err)
	}
	for name, caller := range map[string]*astral.Identity{"the provider": wallet, "this node": node} {
		if err := gate(v.a, caller, wallet); err != nil {
			t.Fatalf("%s was gated: %v", name, err)
		}
	}

	v.auth.grant(v.app, "bitcoin-wallet", node)
	if err := gate(v.a, v.app, wallet); err != nil {
		t.Fatalf("an app holding the permit was refused: %v", err)
	}
	// a permit on another node does not count here
	other := astral.GenerateIdentity()
	v.auth.grant(other, "bitcoin-wallet", v.b.node.Identity())
	if err := gate(v.a, other, wallet); err == nil {
		t.Fatal("a permit for another node reached this node's wallet")
	}
}

// gateQuery runs the query gate on a query string from caller to target.
func gateQuery(m *Module, caller, target *astral.Identity, qs string) error {
	q := astral.Launch(query.New(caller, target, qs, nil))
	return m.PreprocessQuery(core.NewQueryModifier(q))
}

// TestQueryToAPrivateProviderNeedsAPermitForTheServiceItReaches: a provider
// offers two private services; a permit for one reaches that one only, the
// longest name its path names decides, and a path that names neither needs a
// permit for either.
func TestQueryToAPrivateProviderNeedsAPermitForTheServiceItReaches(t *testing.T) {
	v := newVisibilitySwarm()
	_, vault := advertise(t, v.a, "wallet,wallet.keys,notes&open=false", available)
	node := v.a.node.Identity()
	v.auth.grant(v.app, "notes", node)

	for qs, reached := range map[string]bool{
		"notes":              true,
		"notes.read?id=1":    true,
		"wallet.spend":       false,
		"wallet.keys.export": false,
		"other.op":           true,
	} {
		if err := gateQuery(v.a, v.app, vault, qs); (err == nil) != reached {
			t.Fatalf("%s with a notes permit: gate answered %v, want reached %v", qs, err, reached)
		}
	}

	v.auth.grant(v.app, "wallet", node)
	if err := gateQuery(v.a, v.app, vault, "wallet.spend"); err != nil {
		t.Fatalf("wallet.spend with a wallet permit: %v", err)
	}
	if err := gateQuery(v.a, v.app, vault, "wallet.keys.export"); err == nil {
		t.Fatal("wallet.keys.export with a wallet permit only: want refused")
	}
}
