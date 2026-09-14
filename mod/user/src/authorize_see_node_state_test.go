package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func seeNodeStateAction(actor *astral.Identity) *auth.SeeNodeStateAction {
	return &auth.SeeNodeStateAction{Action: auth.NewAction(actor)}
}

// TestSeeNodeStateGrantsUserNodeAndSwarmMembers pins the default holders of
// SeeNodeState on a claimed node: the user identity, this node, and the user's
// unexpelled node members, and nobody else.
//
// why the node members: a remote tree mount queries the target as the mounting
// node's identity (mod/tree.MountRemote), so a sibling mount reads nothing
// without them.
// note: the action covers the log stream, so a node member reads this node's
// logged activity for every caller.
// note: the contract fixtures are the AdminNetwork table's, shared in-package.
func TestSeeNodeStateGrantsUserNodeAndSwarmMembers(t *testing.T) {
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
		{"the user identity", userID, true},
		{"this node", nodeID, true},
		{"a current swarm member", member, true},
		{"an expelled member", expelled, false},
		{"a node in another user's swarm", outsider, false},
		{"a stranger", astral.GenerateIdentity(), false},
		// note: a zero actor must not match the nil identity an unclaimed node answers.
		{"a zero actor", &astral.Identity{}, false},
		{"a nil actor", nil, false},
	}

	for _, c := range cases {
		if got := mod.AuthorizeSeeNodeState(nil, seeNodeStateAction(c.actor)); got != c.want {
			t.Errorf("%s: authorized %v; want %v", c.name, got, c.want)
		}
	}

	// note: the swarm's grant stops at reading. The same member changes nothing.
	if mod.AuthorizeConfigureNodeState(nil, &auth.ConfigureNodeStateAction{Action: auth.NewAction(member)}) {
		t.Error("a swarm member must not change the node's state")
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
