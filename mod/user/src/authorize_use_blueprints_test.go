package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// TestUseBlueprintsGrantsUserAndNodeOnly pins the default holders of
// UseBlueprints on a claimed node: the user identity and this node, and nobody
// else. A current swarm member is refused.
func TestUseBlueprintsGrantsUserAndNodeOnly(t *testing.T) {
	mod, f := swarmFixture(t)

	for _, c := range f.cases() {
		action := &auth.UseBlueprintsAction{Action: auth.NewAction(c.actor)}
		if got := mod.AuthorizeUseBlueprints(nil, action); got != c.want {
			t.Errorf("%s: authorized %v; want %v", c.name, got, c.want)
		}
	}
}

// TestUseBlueprintsGrantsTheNodeOnUnclaimedNode: before any user exists, the
// node's own identity still syncs blueprints, and a stranger still does not.
func TestUseBlueprintsGrantsTheNodeOnUnclaimedNode(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	if !mod.AuthorizeUseBlueprints(nil, &auth.UseBlueprintsAction{Action: auth.NewAction(nodeID)}) {
		t.Fatal("the node must use its own blueprints on an unclaimed node")
	}
	if mod.AuthorizeUseBlueprints(nil, &auth.UseBlueprintsAction{Action: auth.NewAction(astral.GenerateIdentity())}) {
		t.Fatal("a stranger must not use the blueprints of an unclaimed node")
	}
}
