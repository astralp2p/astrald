package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// adminNetworkContracts is an auth module holding a fixed set of signed
// contracts. LocalSwarm reads swarm membership from it; every other method
// panics on the embedded nil interface.
type adminNetworkContracts struct {
	authmod.Module
	contracts []*auth.SignedContract
}

func (a *adminNetworkContracts) SignedContracts() authmod.ContractQueryBuilder {
	return &adminNetworkContractQuery{all: a.contracts}
}

// adminNetworkContractQuery filters by issuer alone.
//
// note: every staged contract is a swarm-membership contract, so WithAction
// narrows nothing here.
type adminNetworkContractQuery struct {
	all    []*auth.SignedContract
	issuer *astral.Identity
}

func (q *adminNetworkContractQuery) WithIssuer(id *astral.Identity) authmod.ContractQueryBuilder {
	q.issuer = id
	return q
}

func (q *adminNetworkContractQuery) WithSubject(*astral.Identity) authmod.ContractQueryBuilder {
	panic("LocalSwarm does not filter by subject")
}

func (q *adminNetworkContractQuery) WithAction(...astral.Object) authmod.ContractQueryBuilder {
	return q
}

func (q *adminNetworkContractQuery) Find(*astral.Context) (found []*auth.SignedContract, _ error) {
	for _, c := range q.all {
		if c.Issuer.IsEqual(q.issuer) {
			found = append(found, c)
		}
	}
	return found, nil
}

func adminNetworkMembership(issuer, subject *astral.Identity) *auth.SignedContract {
	return &auth.SignedContract{Contract: &auth.Contract{Issuer: issuer, Subject: subject}}
}

func adminNetworkAction(actor *astral.Identity) *auth.AdminNetworkAction {
	return &auth.AdminNetworkAction{Action: auth.NewAction(actor)}
}

// TestAdminNetworkGrantsThisNodeBeforeAUserExists is the property the NAT link
// strategy depends on: an unclaimed node has an empty swarm, and it still
// queries its own nat and kcp ops as itself.
func TestAdminNetworkGrantsThisNodeBeforeAUserExists(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	if swarm := mod.LocalSwarm(); len(swarm) != 0 {
		t.Fatalf("an unclaimed node lists a swarm of %d; want none", len(swarm))
	}

	if !mod.AuthorizeAdminNetwork(nil, adminNetworkAction(nodeID)) {
		t.Fatal("this node must administer its own network before a user exists")
	}

	if mod.AuthorizeAdminNetwork(nil, adminNetworkAction(astral.GenerateIdentity())) {
		t.Fatal("a stranger must not administer an unclaimed node's network")
	}
}

// TestAdminNetworkGrantsThisNodeAndCurrentSwarmMembers pins the direct grant on
// a claimed node: this node and the user's unexpelled member nodes, and nobody
// else.
//
// note: the authorizer reads no link table. A linked peer is refused unless it
// is in LocalSwarm, which "a node in another user's swarm" stands for here.
func TestAdminNetworkGrantsThisNodeAndCurrentSwarmMembers(t *testing.T) {
	nodeID, userID := astral.GenerateIdentity(), astral.GenerateIdentity()
	member, expelled := astral.GenerateIdentity(), astral.GenerateIdentity()
	outsider := astral.GenerateIdentity()

	mod := banModule(t, userID)
	mod.node = &identityNode{id: nodeID}
	mod.Deps.Auth = &adminNetworkContracts{contracts: []*auth.SignedContract{
		adminNetworkMembership(userID, nodeID),
		adminNetworkMembership(userID, member),
		adminNetworkMembership(userID, expelled),
		adminNetworkMembership(astral.GenerateIdentity(), outsider),
	}}
	if err := mod.db.StoreExpulsion(sampleSigned(userID, expelled)); err != nil {
		t.Fatalf("store expulsion: %v", err)
	}

	cases := []struct {
		name  string
		actor *astral.Identity
		want  bool
	}{
		{"this node", nodeID, true},
		{"a current swarm member", member, true},
		{"an expelled member", expelled, false},
		{"a node in another user's swarm", outsider, false},
		{"a stranger", astral.GenerateIdentity(), false},
		// note: the rule names nodes only; the user reaches these ops through a grant or a contract.
		{"the user identity", userID, false},
	}

	for _, c := range cases {
		if got := mod.AuthorizeAdminNetwork(nil, adminNetworkAction(c.actor)); got != c.want {
			t.Errorf("%s: authorized %v; want %v", c.name, got, c.want)
		}
	}
}
