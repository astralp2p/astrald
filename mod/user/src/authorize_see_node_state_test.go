package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// TestSeeNodeStateGrantsUserAndNodeOnly pins the default holders of
// SeeNodeState: the user identity and this node's own identity, and nobody
// else.
//
// note: a swarm member is refused too, since the log stream the action covers
// carries every caller's activity.
func TestSeeNodeStateGrantsUserAndNodeOnly(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	userID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	err := mod.config.ActiveContract.Set(nil, &auth.SignedContract{Contract: &auth.Contract{Issuer: userID}})
	if err != nil {
		t.Fatalf("set active contract: %v", err)
	}

	see := func(actor *astral.Identity) bool {
		return mod.AuthorizeSeeNodeState(nil, &auth.SeeNodeStateAction{Action: auth.NewAction(actor)})
	}

	if !see(userID) {
		t.Fatal("the user must read the node's state")
	}

	if !see(nodeID) {
		t.Fatal("the node must read its own state; a local caller carrying no identity is promoted to it")
	}

	if see(astral.GenerateIdentity()) {
		t.Fatal("a caller that is neither the user nor the node must not read the node's state")
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
