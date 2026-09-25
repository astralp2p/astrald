package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func seeNodeStateAction(actor *astral.Identity) *auth.SeeNodeStateAction {
	return &auth.SeeNodeStateAction{Action: auth.NewAction(actor)}
}

// TestSeeNodeStateGrantsUserAndNodeOnly pins the default holders of
// SeeNodeState on a claimed node: the user identity and this node, and nobody
// else. A current swarm member is refused.
//
// note: the contract fixtures are the AdminNetwork table's, shared in-package.
func TestSeeNodeStateGrantsUserAndNodeOnly(t *testing.T) {
	mod, f := swarmFixture(t)

	for _, c := range f.cases() {
		if got := mod.AuthorizeSeeNodeState(nil, seeNodeStateAction(c.actor)); got != c.want {
			t.Errorf("%s: authorized %v; want %v", c.name, got, c.want)
		}
	}
}

// TestSeeNodeStateGrantsTheNodeOnUnclaimedNode: before any user exists, the
// node's own identity still reads its state, and a stranger still does not.
func TestSeeNodeStateGrantsTheNodeOnUnclaimedNode(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	if !mod.AuthorizeSeeNodeState(nil, &auth.SeeNodeStateAction{Action: auth.NewAction(nodeID)}) {
		t.Fatal("the node must read its own state on an unclaimed node")
	}

	if mod.AuthorizeSeeNodeState(nil, &auth.SeeNodeStateAction{Action: auth.NewAction(astral.GenerateIdentity())}) {
		t.Fatal("a stranger must not read the state of an unclaimed node")
	}
}

// swarmIDs holds the identities of a claimed node's swarm fixture.
type swarmIDs struct {
	user, node, member, expelled, outsider *astral.Identity
}

type authzCase struct {
	name  string
	actor *astral.Identity
	want  bool
}

// cases lists every actor kind and the default rule user + this node expects.
func (f swarmIDs) cases() []authzCase {
	return []authzCase{
		{"the user identity", f.user, true},
		{"this node", f.node, true},
		{"a current swarm member", f.member, false},
		{"an expelled member", f.expelled, false},
		{"a node in another user's swarm", f.outsider, false},
		{"a stranger", astral.GenerateIdentity(), false},
		// note: a zero actor must not match the nil identity an unclaimed node answers.
		{"a zero actor", &astral.Identity{}, false},
		{"a nil actor", nil, false},
	}
}

// swarmFixture builds a claimed node whose swarm holds a current member, an
// expelled member, and a node of another user's swarm.
func swarmFixture(t *testing.T) (*Module, swarmIDs) {
	t.Helper()
	f := swarmIDs{
		user: astral.GenerateIdentity(), node: astral.GenerateIdentity(),
		member: astral.GenerateIdentity(), expelled: astral.GenerateIdentity(),
		outsider: astral.GenerateIdentity(),
	}

	mod := banModule(t, f.user)
	mod.node = &identityNode{id: f.node}
	mod.Deps.Auth = &adminNetworkContracts{contracts: []*auth.SignedContract{
		adminNetworkMembership(f.user, f.node),
		adminNetworkMembership(f.user, f.member),
		adminNetworkMembership(f.user, f.expelled),
		adminNetworkMembership(astral.GenerateIdentity(), f.outsider),
	}}
	if err := mod.db.StoreExpulsion(sampleSigned(f.user, f.expelled)); err != nil {
		t.Fatalf("store expulsion: %v", err)
	}

	// note: the fixture is live only if the member is in the swarm.
	if !slicesContainsIdentity(mod.LocalSwarm(), f.member) {
		t.Fatal("fixture: the current member is not in LocalSwarm")
	}

	return mod, f
}

func slicesContainsIdentity(ids []*astral.Identity, id *astral.Identity) bool {
	for _, x := range ids {
		if x.IsEqual(id) {
			return true
		}
	}
	return false
}
