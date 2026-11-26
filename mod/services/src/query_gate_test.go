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
