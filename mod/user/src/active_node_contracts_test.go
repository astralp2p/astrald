package user

import (
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func TestActiveNodeContractsAndNodes(t *testing.T) {
	userID, n1, n2 := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	c1, c2 := adminNetworkMembership(userID, n1), adminNetworkMembership(userID, n2)
	foreign := adminNetworkMembership(astral.GenerateIdentity(), astral.GenerateIdentity())

	cases := []struct {
		name          string
		expel         []*astral.Identity
		wantContracts []*auth.SignedContract
		wantNodes     []*astral.Identity
	}{
		{"no expulsions", nil, []*auth.SignedContract{c1, c2}, []*astral.Identity{n1, n2}},
		{"an expelled node", []*astral.Identity{n2}, []*auth.SignedContract{c1}, []*astral.Identity{n1}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mod := &Module{db: testDB(t)}
			mod.Deps.Auth = &adminNetworkContracts{contracts: []*auth.SignedContract{c1, c2, foreign}}
			for _, id := range c.expel {
				if err := mod.db.StoreExpulsion(sampleSigned(userID, id)); err != nil {
					t.Fatalf("store expulsion: %v", err)
				}
			}

			contracts, err := mod.ActiveNodeContracts(userID)
			if err != nil {
				t.Fatalf("ActiveNodeContracts: %v", err)
			}
			if !slices.Equal(contracts, c.wantContracts) {
				t.Errorf("ActiveNodeContracts subjects = %v, want %v", subjects(contracts), subjects(c.wantContracts))
			}

			nodes := mod.ActiveNodes(userID)
			if !slices.EqualFunc(nodes, c.wantNodes, (*astral.Identity).IsEqual) {
				t.Errorf("ActiveNodes = %v, want %v", nodes, c.wantNodes)
			}
		})
	}
}

func subjects(contracts []*auth.SignedContract) (list []*astral.Identity) {
	for _, c := range contracts {
		list = append(list, c.Subject)
	}
	return
}
